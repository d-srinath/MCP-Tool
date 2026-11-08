// MCP client UI (v1) — Go implementation.
//
// Stdlib web server on 127.0.0.1:8090. Every answer comes from calling the
// MCP server (http://127.0.0.1:8000/mcp) with the official Go MCP SDK
// client; this app only routes the typed command to the right MCP tool.
//
// Port of the original Python app.py: same routes, same replies. Branding
// per Srinath (2026-11-08): the page is the "Paas Portal" — heading
// "Welcome to Paas Portal", and the environments are NOT displayed upfront;
// they are listed only when asked (e.g. "list clusters").
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpURL = "http://127.0.0.1:8000/mcp"

// callTool performs one MCP tool call, like the Python FastMCP Client did:
// a fresh client session per call.
func callTool(name string, args map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "paas-portal-client", Version: "1.0.0"}, nil)
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

func route(message string) chatReply {
	text := strings.ToLower(message)
	cluster := findCluster(text)
	reply, err := routeInner(text, cluster)
	if err != nil {
		return chatReply{Reply: fmt.Sprintf("Something went wrong talking to the MCP server: %v", err)}
	}
	return reply
}

func routeInner(text, cluster string) (chatReply, error) {
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
			return chatReply{
				Reply: fmt.Sprintf("Opening the OpenShift login page for %s. "+
					"Choose the 'ldap' provider and sign in with your own account "+
					"(srinath-admin or srinath-developer, dev password changeme123). "+
					"OpenShift will give you exactly your group's permissions. "+
					"If the console opens without asking, log out (top-right) first, then log in again and pick 'ldap'.", cluster),
				ActionURL:   data.URL,
				ActionLabel: "Open the OpenShift login page",
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
		"• 'connect to sandbox' or 'connect to local' — opens the real OpenShift (LDAP) login page\n" +
		"• 'status of sandbox cluster'"}, nil
}

const pageHTML = `<!doctype html><html><head><meta charset="utf-8"><title>Paas Portal</title>
<style>
body{font-family:-apple-system,Helvetica,Arial,sans-serif;margin:0;background:#f4f5f7;color:#1b1e23}
header{background:#15181d;color:#fff;padding:18px 28px} header h1{margin:0;font-size:20px} header p{margin:4px 0 0;color:#9ba1a9;font-size:13px}
.wrap{max-width:880px;margin:24px auto;padding:0 16px}
#chat{background:#fff;border-radius:10px;box-shadow:0 1px 3px rgba(0,0,0,.08);padding:18px}
#log{height:300px;overflow:auto;display:flex;flex-direction:column;gap:10px;margin-bottom:12px}
.msg{padding:10px 13px;border-radius:10px;white-space:pre-wrap;max-width:85%;font-size:14px;line-height:1.45}
.me{align-self:flex-end;background:#dbeafe}
.bot{align-self:flex-start;background:#f1f1f1}
form{display:flex;gap:8px} input{flex:1;padding:11px;border:1px solid #ccc;border-radius:8px;font-size:14px}
button{padding:11px 18px;border:0;border-radius:8px;background:#c00;color:#fff;font-size:14px;cursor:pointer}
</style></head><body>
<header><h1>Welcome to Paas Portal</h1><p>Answers come from the MCP server's tools.</p></header>
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
add('Hello Srinath. Ask me to list the clusters, or say: connect me to sandbox cluster.','bot');
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
		writeJSON(w, http.StatusOK, route(payload.Message))
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
