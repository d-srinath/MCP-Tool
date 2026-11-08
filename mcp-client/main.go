// MCP client UI (v2) — Go implementation ("Paas Portal").
//
// Stdlib web server on 127.0.0.1:8090. Every answer comes from calling the
// MCP server (http://127.0.0.1:8000/mcp) with the official Go MCP SDK
// client; this app only routes the typed command to the right MCP tool.
//
// v2 (2026-11-08): real per-user sign-in. "Connect me to sandbox" offers a
// Sign in link that starts an OpenShift OAuth flow (OAuthClient
// "paas-portal", LDAP login page). After login the portal holds the
// user's OWN OpenShift token server-side (in-memory, keyed by a random
// session cookie) and passes it to the MCP work tools, so everything the
// user asks for in chat runs with exactly their LDAP/RBAC permissions:
// a developer sees only the dev project, an admin sees everything.
//
// Branding per Srinath (2026-11-08): the page is the "Paas Portal" —
// heading "Welcome to Paas Portal", and the environments are NOT
// displayed upfront; they are listed only when asked (e.g. "list
// clusters").
package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpURL = "http://127.0.0.1:8000/mcp"

// Sandbox (local CRC) endpoints for the OAuth flow.
const (
	oauthAuthorizeURL = "https://oauth-openshift.apps-crc.testing/oauth/authorize"
	oauthTokenURL     = "https://oauth-openshift.apps-crc.testing/oauth/token"
	apiBaseURL        = "https://api.crc.testing:6443"
	oauthClientID     = "paas-portal"
	redirectURI       = "http://localhost:8090/auth/callback"
)

// insecureClient talks to the local CRC endpoints, which serve a
// self-signed certificate, so TLS verification is skipped. The
// organization version must use the cluster's real CA instead.
var insecureClient = &http.Client{
	Timeout:   30 * time.Second,
	Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // local CRC self-signed cert only
}

// oauthSecret returns the paas-portal OAuthClient secret: from the
// PAAS_OAUTH_SECRET environment variable, else from
// ~/.paas-portal/oauth-secret. The secret is never in source or git —
// it is generated on the cluster at install time and stored outside
// the repo (see manifests/oauthclient-paas-portal.yaml).
func oauthSecret() string {
	if s := strings.TrimSpace(os.Getenv("PAAS_OAUTH_SECRET")); s != "" {
		return s
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, ".paas-portal", "oauth-secret"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// ---- sessions: the user's token lives server-side only ----

type session struct {
	Token    string
	Username string
}

var (
	sessionsMu sync.Mutex
	sessions   = map[string]*session{}
)

func newSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func sessionFromRequest(r *http.Request) *session {
	c, err := r.Cookie("paas_session")
	if err != nil || c.Value == "" {
		return nil
	}
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	return sessions[c.Value]
}

// ---- OAuth handlers ----

func authLogin(w http.ResponseWriter, r *http.Request) {
	loc := oauthAuthorizeURL + "?client_id=" + oauthClientID +
		"&response_type=code&scope=user%3Afull&redirect_uri=" +
		url.QueryEscape(redirectURI)
	http.Redirect(w, r, loc, http.StatusFound)
}

func authCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Sign-in failed: no authorization code returned by OpenShift.", http.StatusBadRequest)
		return
	}
	secret := oauthSecret()
	if secret == "" {
		http.Error(w, "Sign-in is not configured on the portal: the paas-portal OAuth secret is missing (set PAAS_OAUTH_SECRET or ~/.paas-portal/oauth-secret).", http.StatusInternalServerError)
		return
	}
	// Exchange the code for the user's access token.
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
	}
	req, err := http.NewRequest(http.MethodPost, oauthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		http.Error(w, "Sign-in failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(oauthClientID, secret)
	resp, err := insecureClient.Do(req)
	if err != nil {
		http.Error(w, "Sign-in failed talking to OpenShift: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Printf("paas-portal: token exchange failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		http.Error(w, fmt.Sprintf("Sign-in failed: token exchange returned HTTP %d.", resp.StatusCode), http.StatusBadGateway)
		return
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		http.Error(w, "Sign-in failed: no access token in the OpenShift response.", http.StatusBadGateway)
		return
	}
	// Learn who signed in.
	username, err := fetchUsername(tok.AccessToken)
	if err != nil || username == "" {
		http.Error(w, "Sign-in failed: could not read your OpenShift user.", http.StatusBadGateway)
		return
	}
	sid := newSessionID()
	sessionsMu.Lock()
	sessions[sid] = &session{Token: tok.AccessToken, Username: username}
	sessionsMu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "paas_session", Value: sid, Path: "/", HttpOnly: true})
	http.Redirect(w, r, "/", http.StatusFound)
}

func fetchUsername(token string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, apiBaseURL+"/apis/user.openshift.io/v1/users/~", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := insecureClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("users/~ returned HTTP %d", resp.StatusCode)
	}
	var u struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&u); err != nil {
		return "", err
	}
	return u.Metadata.Name, nil
}

func authLogout(w http.ResponseWriter, r *http.Request) {
	// Local sign-out only: drop the server-side session and the cookie.
	// The OpenShift SSO session itself is left alone (the user can log
	// out of the OpenShift console separately if they want).
	if c, err := r.Cookie("paas_session"); err == nil {
		sessionsMu.Lock()
		delete(sessions, c.Value)
		sessionsMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "paas_session", Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusFound)
}

func apiMe(w http.ResponseWriter, r *http.Request) {
	if sess := sessionFromRequest(r); sess != nil {
		writeJSON(w, http.StatusOK, map[string]any{"signed_in": true, "username": sess.Username})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"signed_in": false})
}

// callTool performs one MCP tool call, like the Python FastMCP Client did:
// a fresh client session per call.
func callTool(name string, args map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "paas-portal-client", Version: "2.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: mcpURL}, nil)
	if err != nil {
		return "", err
	}
	defer session.Close()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return "", err
	}
	if len(res.Content) == 0 {
		return "", fmt.Errorf("empty result from tool %s", name)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		return "", fmt.Errorf("unexpected content type from tool %s", name)
	}
	return tc.Text, nil
}

func findCluster(text string) string {
	t := strings.ToLower(text)
	switch {
	case strings.Contains(t, "sandbox") || strings.Contains(t, "local") || strings.Contains(t, "crc"):
		return "sandbox"
	case strings.Contains(t, "production") || strings.Contains(t, "prod"):
		return "production"
	case strings.Contains(t, "dev"):
		return "dev"
	}
	return ""
}

type chatReply struct {
	Reply       string `json:"reply"`
	ActionURL   string `json:"action_url,omitempty"`
	ActionLabel string `json:"action_label,omitempty"`
}

type clusterInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Configured  bool   `json:"configured"`
}

func signInPrompt() chatReply {
	return chatReply{
		Reply:       "Sign in to sandbox first — then everything you ask me to do runs with your own OpenShift permissions, nothing more.",
		ActionURL:   "/auth/login",
		ActionLabel: "Sign in to sandbox",
	}
}

var (
	reInNamespace  = regexp.MustCompile(`\bin\s+([a-z0-9][a-z0-9\-]*)`)
	reDeployTarget = regexp.MustCompile(`deployment\s+([a-z0-9][a-z0-9\-.]*)`)
	rePodsOf       = regexp.MustCompile(`pods?\s+(?:of\s+|for\s+)?(?:deployment\s+)?([a-z0-9][a-z0-9\-.]*)`)
)

// parsePodsTarget extracts the deployment name from phrasings like
// "delete pods of app1 in ns", "delete the pods of deployment app1 in
// ns" or "delete pod app1 in ns", plus the namespace after "in".
func parsePodsTarget(text string) (name, namespace string) {
	if m := rePodsOf.FindStringSubmatch(text); m != nil {
		name = strings.TrimSuffix(m[1], ".")
		if name == "in" {
			name = ""
		}
	}
	if m := reInNamespace.FindStringSubmatch(text); m != nil {
		namespace = m[1]
	}
	return name, namespace
}

func parseDeployTarget(text string) (name, namespace string) {
	if m := reDeployTarget.FindStringSubmatch(text); m != nil {
		name = strings.TrimSuffix(m[1], ".")
	}
	if m := reInNamespace.FindStringSubmatch(text); m != nil {
		namespace = m[1]
	}
	return name, namespace
}

// workTool runs one of the v2 token tools for a signed-in user and wraps
// the plain-text result as a chat reply.
func workTool(tool string, args map[string]any, header string) (chatReply, error) {
	raw, err := callTool(tool, args)
	if err != nil {
		return chatReply{}, err
	}
	if header != "" && !strings.HasPrefix(raw, "Not allowed") {
		return chatReply{Reply: header + "\n" + raw}, nil
	}
	return chatReply{Reply: raw}, nil
}

func route(message string, sess *session) chatReply {
	text := strings.ToLower(strings.TrimSpace(message))
	cluster := findCluster(text)
	reply, err := routeInner(text, cluster, sess)
	if err != nil {
		return chatReply{Reply: fmt.Sprintf("Something went wrong talking to the MCP server: %v", err)}
	}
	return reply
}

func routeInner(text, cluster string, sess *session) (chatReply, error) {
	// "delete pods of <deployment> in <namespace>" — deletes the pods
	// only; the deployment recreates them. (Checked before the
	// deployment-deletion refusal below, because this phrasing often
	// contains the word "deployment" too.)
	if strings.Contains(text, "delete") && strings.Contains(text, "pod") {
		if sess == nil {
			return signInPrompt(), nil
		}
		name, ns := parsePodsTarget(text)
		if name == "" {
			return chatReply{Reply: "Which deployment's pods should I delete? Say: delete pods of <deployment> in <namespace>"}, nil
		}
		if ns == "" {
			return chatReply{Reply: fmt.Sprintf("Which namespace is deployment %s in? Say: delete pods of %s in <namespace>", name, name)}, nil
		}
		return workTool("delete_pods", map[string]any{"token": sess.Token, "namespace": ns, "name": name}, "")
	}
	// "delete deployment ..." — refused by design (Srinath, 2026-11-08):
	// deleting a deployment removes the whole application. The tool
	// deletes pods only; deployments are never deleted here.
	if strings.Contains(text, "delete") && strings.Contains(text, "deployment") {
		if sess == nil {
			return signInPrompt(), nil
		}
		return chatReply{Reply: "I don't delete deployments — that would remove the whole application. What I can do is delete a deployment's pods (it recreates them right away). Say: delete pods of <deployment> in <namespace>"}, nil
	}
	// "list pods" (optionally "in <namespace>").
	if strings.Contains(text, "pod") {
		if sess == nil {
			return signInPrompt(), nil
		}
		args := map[string]any{"token": sess.Token}
		if m := reInNamespace.FindStringSubmatch(text); m != nil {
			args["namespace"] = m[1]
		}
		return workTool("list_pods", args, "Pods:")
	}
	// "restart deployment <name> in <namespace>".
	if strings.Contains(text, "restart") && strings.Contains(text, "deployment") {
		if sess == nil {
			return signInPrompt(), nil
		}
		name, ns := parseDeployTarget(text)
		if name == "" {
			return chatReply{Reply: "Which deployment should I restart? Say: restart deployment <name> in <namespace>"}, nil
		}
		if ns == "" {
			return chatReply{Reply: fmt.Sprintf("Which namespace is deployment %s in? Say: restart deployment %s in <namespace>", name, name)}, nil
		}
		return workTool("restart_deployment", map[string]any{"token": sess.Token, "namespace": ns, "name": name}, "")
	}
	// "list namespaces" / "what are all the namespaces" / "list projects".
	if strings.Contains(text, "namespace") || strings.Contains(text, "project") {
		if sess == nil {
			return signInPrompt(), nil
		}
		return workTool("list_namespaces", map[string]any{"token": sess.Token}, "Namespaces you can see:")
	}
	// "list groups" / "what are all the groups".
	if strings.Contains(text, "group") {
		if sess == nil {
			return signInPrompt(), nil
		}
		return workTool("list_groups", map[string]any{"token": sess.Token}, "Groups:")
	}
	// "list deployments" (optionally "in <namespace>").
	if strings.Contains(text, "deployment") {
		if sess == nil {
			return signInPrompt(), nil
		}
		args := map[string]any{"token": sess.Token}
		if m := reInNamespace.FindStringSubmatch(text); m != nil {
			args["namespace"] = m[1]
		}
		return workTool("list_deployments", args, "Deployments:")
	}
	if strings.Contains(text, "list") || strings.Contains(text, "what are") || strings.Contains(text, "which cluster") || strings.Contains(text, "all the clusters") {
		raw, err := callTool("list_clusters", map[string]any{})
		if err != nil {
			return chatReply{}, err
		}
		var data []clusterInfo
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			return chatReply{}, err
		}
		lines := []string{"Here are all the clusters I know:"}
		for _, c := range data {
			state := "ready"
			if !c.Configured {
				state = "not configured yet"
			}
			lines = append(lines, fmt.Sprintf("• %s — %s (%s)", c.Name, c.Description, state))
		}
		return chatReply{Reply: strings.Join(lines, "\n")}, nil
	}
	if strings.Contains(text, "connect") || strings.Contains(text, "log in") || strings.Contains(text, "login") {
		if cluster == "" {
			return chatReply{Reply: "Which cluster — sandbox, dev, or production? Try: 'connect to sandbox'."}, nil
		}
		raw, err := callTool("login_url", map[string]any{"name": cluster})
		if err != nil {
			return chatReply{}, err
		}
		var data struct {
			Configured bool   `json:"configured"`
			URL        string `json:"url"`
			Reason     string `json:"reason"`
			Error      string `json:"error"`
		}
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			return chatReply{}, err
		}
		if data.Configured {
			// Sandbox: sign in through the portal's own OAuth flow so
			// the chat can act afterwards with the user's own token.
			if sess != nil {
				return chatReply{Reply: fmt.Sprintf("You're signed in as %s — ask me to list namespaces or deployments.", sess.Username)}, nil
			}
			return chatReply{
				Reply:       fmt.Sprintf("To connect to %s, sign in with your OpenShift account — choose the 'ldap' provider on the login page. After signing in you'll be back here, and everything you ask me to do will run with exactly your group's permissions.", cluster),
				ActionURL:   "/auth/login",
				ActionLabel: "Sign in to sandbox",
			}, nil
		}
		reason := data.Reason
		if reason == "" {
			reason = data.Error
		}
		return chatReply{Reply: fmt.Sprintf("Cannot connect to %s yet: %s", cluster, reason)}, nil
	}
	if strings.Contains(text, "status") || strings.Contains(text, "health") {
		target := cluster
		if target == "" {
			target = "sandbox"
		}
		raw, err := callTool("cluster_status", map[string]any{"name": target})
		if err != nil {
			return chatReply{}, err
		}
		var data map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			return chatReply{}, err
		}
		if _, ok := data["status"]; ok {
			var st struct {
				Status          string `json:"status"`
				ProjectsVisible any    `json:"projects_visible"`
			}
			_ = json.Unmarshal([]byte(raw), &st)
			projects := st.ProjectsVisible
			if projects == nil {
				projects = "?"
			}
			return chatReply{Reply: fmt.Sprintf("Status of %s:\n%s\nProjects visible: %v", target, st.Status, projects)}, nil
		}
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal([]byte(raw), &e)
		errText := e.Error
		if errText == "" {
			errText = "unknown"
		}
		return chatReply{Reply: fmt.Sprintf("%s: %s", target, errText)}, nil
	}
	return chatReply{Reply: "I can do these today:\n" +
		"• 'list clusters' / 'what are all the clusters'\n" +
		"• 'connect to sandbox' or 'connect to local' — sign in with your OpenShift (LDAP) account\n" +
		"• once signed in: 'list namespaces', 'list groups', 'list deployments'\n" +
		"• 'restart deployment <name> in <namespace>', 'list pods in <namespace>' and 'delete pods of <name> in <namespace>' (deployments themselves are never deleted)\n" +
		"• 'status of sandbox cluster'"}, nil
}

const pageHTML = `<!doctype html><html><head><meta charset="utf-8"><title>Paas Portal</title>
<style>
body{font-family:-apple-system,Helvetica,Arial,sans-serif;margin:0;background:#f4f5f7;color:#1b1e23}
header{background:#15181d;color:#fff;padding:18px 28px;display:flex;justify-content:space-between;align-items:flex-start} header h1{margin:0;font-size:20px} header p{margin:4px 0 0;color:#9ba1a9;font-size:13px}
.badge{border:1px solid #4a4f57;border-radius:999px;padding:5px 12px;font-size:12px;color:#e6e8eb;white-space:nowrap;margin-top:2px}
#userbox{font-size:13px;color:#e6e8eb;white-space:nowrap;margin-top:4px}
#userbox a{color:#e6e8eb}
.wrap{max-width:880px;margin:24px auto;padding:0 16px}
#chat{background:#fff;border-radius:10px;box-shadow:0 1px 3px rgba(0,0,0,.08);padding:18px}
#log{height:300px;overflow:auto;display:flex;flex-direction:column;gap:10px;margin-bottom:12px}
.msg{padding:10px 13px;border-radius:10px;white-space:pre-wrap;max-width:85%;font-size:14px;line-height:1.45}
.me{align-self:flex-end;background:#dbeafe}
.bot{align-self:flex-start;background:#f1f1f1}
form{display:flex;gap:8px} input{flex:1;padding:11px;border:1px solid #ccc;border-radius:8px;font-size:14px}
button{padding:11px 18px;border:0;border-radius:8px;background:#c00;color:#fff;font-size:14px;cursor:pointer}
</style></head><body>
<header><div><h1>Welcome to Paas Portal</h1><p>Answers come from the MCP server's tools.</p></div><div style="display:flex;gap:12px;align-items:flex-start"><span id="userbox"></span><div class="badge">MCP Paas Tool</div></div></header>
<div class="wrap">
<div id="chat"><div id="log"></div>
<form id="f"><input id="i" placeholder="Try: list clusters — or — connect me to sandbox cluster" autocomplete="off"><button>Send</button></form>
</div></div>
<script>
function add(text, cls){ const d=document.createElement('div'); d.className='msg '+cls; d.textContent=text; const log=document.getElementById('log'); log.appendChild(d); log.scrollTop=log.scrollHeight; }
document.getElementById('f').addEventListener('submit', async e => {
  e.preventDefault(); const inp=document.getElementById('i'); const text=inp.value.trim(); if(!text) return;
  add(text,'me'); inp.value=''; add('Working on it…','bot'); const pending=document.querySelector('#log .bot:last-child');
  const r = await fetch('/api/chat', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({message:text})});
  const data = await r.json(); pending.textContent = data.reply;
  if (data.action_url){ window.open(data.action_url, '_blank'); const a=document.createElement('a'); a.href=data.action_url; a.target='_blank'; a.textContent=data.action_label||'Open'; a.style.cssText='display:inline-block;margin-top:8px;padding:8px 14px;background:#c00;color:#fff;border-radius:8px;text-decoration:none'; pending.appendChild(document.createElement('br')); pending.appendChild(a); }
});
add('Hello! Welcome to Paas Portal. Ask me to list the clusters, or say: connect me to sandbox cluster.','bot');
fetch('/api/me').then(r=>r.json()).then(me=>{ if(me.signed_in){ document.getElementById('userbox').innerHTML = me.username + ' · <a href="/auth/logout">Sign out</a>'; add('Signed in as '+me.username+'. You can ask me to list namespaces, list deployments, restart a deployment, and more.','bot'); } }).catch(()=>{});
</script></body></html>`

func writeJSON(w http.ResponseWriter, code int, v any) {
	body, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

func handler(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/auth/login":
		authLogin(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/auth/callback":
		authCallback(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/auth/logout":
		authLogout(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/me":
		apiMe(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/":
		body := []byte(pageHTML)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	case r.Method == http.MethodGet && r.URL.Path == "/api/clusters":
		raw, err := callTool("list_clusters", map[string]any{})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		var data []clusterInfo
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, data)
	case r.Method == http.MethodPost && r.URL.Path == "/api/chat":
		var payload struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		writeJSON(w, http.StatusOK, route(payload.Message, sessionFromRequest(r)))
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)
	log.Println("Paas Portal (Go MCP client) listening on http://127.0.0.1:8090")
	log.Fatal(http.ListenAndServe("127.0.0.1:8090", mux))
}
