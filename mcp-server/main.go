// OpenShift MCP server (v2) — Go implementation.
//
// Exposes cluster tools over MCP (streamable HTTP on 127.0.0.1:8000/mcp).
// Runs on the local Mac, where the local CRC cluster (sandbox) resolves.
// Clusters 'dev' and 'production' are declared but not configured yet --
// they arrive in the organization phase. Tools answer honestly about that.
//
// v1 tools (list_clusters, connect_cluster, cluster_status, login_url)
// keep their original behavior and JSON outputs. v2 adds work tools
// (list_namespaces, list_groups, list_deployments, list_pods,
// restart_deployment, delete_pods) that call the OpenShift API with the
// signed-in
// USER'S OWN bearer token, so LDAP group / RBAC permissions apply
// exactly to everything they do.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type clusterInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Configured  bool   `json:"configured"`
}

var clusterList = []clusterInfo{
	{Name: "sandbox", Description: "Local OpenShift CRC cluster on this machine", Kind: "local-crc", Configured: true},
	{Name: "dev", Description: "Organization dev cluster (not connected yet)", Kind: "organization", Configured: false},
	{Name: "production", Description: "Organization production cluster (not connected yet)", Kind: "organization", Configured: false},
}

var clusters = func() map[string]clusterInfo {
	m := make(map[string]clusterInfo, len(clusterList))
	for _, c := range clusterList {
		m[c.Name] = c
	}
	return m
}()

// oc runs a command on the Mac inside the CRC oc environment, exactly like
// the Python version: /bin/zsh -lc "eval $(crc oc-env); <script>".
func oc(script string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, "/bin/zsh", "-lc", "eval $(crc oc-env); "+script).Output()
	return strings.TrimSpace(string(out))
}

type sandboxConnect struct {
	Cluster   string `json:"cluster"`
	Connected bool   `json:"connected"`
	User      string `json:"user"`
	Server    string `json:"server"`
	OpenShift string `json:"openshift"`
	Node      string `json:"node"`
}

func sandboxConnectResult() sandboxConnect {
	whoami := oc("oc whoami")
	server := oc("oc whoami --show-server")
	node := oc("oc get nodes --no-headers | head -1")
	version := ""
	if whoami != "" {
		version = oc("crc status | grep -i openshift")
	}
	return sandboxConnect{
		Cluster:   "sandbox",
		Connected: whoami != "",
		User:      whoami,
		Server:    server,
		OpenShift: version,
		Node:      node,
	}
}

func textResult(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
}

type nameArgs struct {
	Name string `json:"name"`
}

type emptyArgs struct{}

func normalize(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "prod" {
		key = "production"
	}
	return key
}

func listClusters(ctx context.Context, req *mcp.CallToolRequest, _ emptyArgs) (*mcp.CallToolResult, any, error) {
	return textResult(clusterList)
}

type connectUnknown struct {
	Connected bool   `json:"connected"`
	Error     string `json:"error"`
}

type connectNotConfigured struct {
	Cluster   string `json:"cluster"`
	Connected bool   `json:"connected"`
	Reason    string `json:"reason"`
}

func connectCluster(ctx context.Context, req *mcp.CallToolRequest, args nameArgs) (*mcp.CallToolResult, any, error) {
	key := normalize(args.Name)
	info, ok := clusters[key]
	if !ok {
		return textResult(connectUnknown{Connected: false, Error: "unknown cluster '" + args.Name + "'"})
	}
	if !info.Configured {
		return textResult(connectNotConfigured{
			Cluster:   key,
			Connected: false,
			Reason:    "This cluster is not configured yet. It will be connected in the organization phase.",
		})
	}
	return textResult(sandboxConnectResult())
}

type loginUnknown struct {
	Error string `json:"error"`
}

type loginNotConfigured struct {
	Cluster    string `json:"cluster"`
	Configured bool   `json:"configured"`
	Reason     string `json:"reason"`
}

type loginOK struct {
	Cluster    string `json:"cluster"`
	Configured bool   `json:"configured"`
	URL        string `json:"url"`
	Provider   string `json:"provider"`
	Note       string `json:"note"`
}

func loginURL(ctx context.Context, req *mcp.CallToolRequest, args nameArgs) (*mcp.CallToolResult, any, error) {
	key := normalize(args.Name)
	if key == "local" || key == "crc" || key == "local openshift" {
		key = "sandbox"
	}
	info, ok := clusters[key]
	if !ok {
		return textResult(loginUnknown{Error: "unknown cluster '" + args.Name + "'"})
	}
	if !info.Configured {
		return textResult(loginNotConfigured{
			Cluster:    key,
			Configured: false,
			Reason:     "This cluster is not configured yet, so it has no login page yet. It arrives in the organization phase.",
		})
	}
	return textResult(loginOK{
		Cluster:    key,
		Configured: true,
		URL:        "https://console-openshift-console.apps-crc.testing",
		Provider:   "ldap",
		Note:       "Log in choosing the 'ldap' provider (e.g. testadmin1 or testdeveloper1). Permissions come from your LDAP group: admins = cluster-admin (everything), developers = view in the dev project only.",
	})
}

type statusUnknown struct {
	Error string `json:"error"`
}

type statusNotConfigured struct {
	Cluster string `json:"cluster"`
	Status  string `json:"status"`
}

type statusOK struct {
	Cluster         string `json:"cluster"`
	Status          string `json:"status"`
	ProjectsVisible string `json:"projects_visible"`
}

func clusterStatus(ctx context.Context, req *mcp.CallToolRequest, args nameArgs) (*mcp.CallToolResult, any, error) {
	key := normalize(args.Name)
	info, ok := clusters[key]
	if !ok {
		return textResult(statusUnknown{Error: "unknown cluster '" + args.Name + "'"})
	}
	if !info.Configured {
		return textResult(statusNotConfigured{Cluster: key, Status: "not configured yet"})
	}
	return textResult(statusOK{
		Cluster:         key,
		Status:          oc("crc status"),
		ProjectsVisible: oc("oc get projects --no-headers | wc -l"),
	})
}

// ---- v2 work tools: act with the user's own OpenShift token ----

const apiBase = "https://api.crc.testing:6443"

// Local CRC serves a self-signed certificate, so TLS verification is
// skipped for these local calls. The organization version must use the
// cluster's real CA instead of InsecureSkipVerify.
var apiClient = &http.Client{
	Timeout:   30 * time.Second,
	Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // local CRC self-signed cert only
}

func plainTextResult(s string) (*mcp.CallToolResult, any, error) {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}, nil, nil
}

func apiCall(method, path, token string, body []byte, contentType string) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = strings.NewReader(string(body))
	}
	req, err := http.NewRequest(method, apiBase+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := apiClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return resp.StatusCode, b, err
}

// statusMessage extracts the Kubernetes Status message from an error body.
func statusMessage(body []byte) string {
	var st struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &st); err == nil && st.Message != "" {
		return st.Message
	}
	return strings.TrimSpace(string(body))
}

func needToken(token string) string {
	if strings.TrimSpace(token) == "" {
		return "No OpenShift token was provided. Sign in through the Paas Portal first, then try again."
	}
	return ""
}

// fetchProjects returns the project names the token's owner can see.
func fetchProjects(token string) ([]string, int, string) {
	code, body, err := apiCall(http.MethodGet, "/apis/project.openshift.io/v1/projects", token, nil, "")
	if err != nil {
		return nil, 0, err.Error()
	}
	if code != http.StatusOK {
		return nil, code, statusMessage(body)
	}
	var res struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, code, err.Error()
	}
	names := make([]string, 0, len(res.Items))
	for _, it := range res.Items {
		names = append(names, it.Metadata.Name)
	}
	return names, code, ""
}

type tokenArgs struct {
	Token string `json:"token"`
}

func listNamespaces(ctx context.Context, req *mcp.CallToolRequest, args tokenArgs) (*mcp.CallToolResult, any, error) {
	if msg := needToken(args.Token); msg != "" {
		return plainTextResult(msg)
	}
	names, code, msg := fetchProjects(args.Token)
	if code == http.StatusForbidden {
		return plainTextResult("Not allowed: your account cannot list namespaces/projects.")
	}
	if code != http.StatusOK {
		return plainTextResult(fmt.Sprintf("Could not list namespaces (HTTP %d): %s", code, msg))
	}
	if len(names) == 0 {
		return plainTextResult("No namespaces are visible to your account.")
	}
	return plainTextResult(strings.Join(names, "\n"))
}

func listGroups(ctx context.Context, req *mcp.CallToolRequest, args tokenArgs) (*mcp.CallToolResult, any, error) {
	if msg := needToken(args.Token); msg != "" {
		return plainTextResult(msg)
	}
	code, body, err := apiCall(http.MethodGet, "/apis/user.openshift.io/v1/groups", args.Token, nil, "")
	if err != nil {
		return plainTextResult("Could not reach the cluster API: " + err.Error())
	}
	if code == http.StatusForbidden {
		return plainTextResult("Not allowed: your account cannot list groups (needs cluster permissions).")
	}
	if code != http.StatusOK {
		return plainTextResult(fmt.Sprintf("Could not list groups (HTTP %d): %s", code, statusMessage(body)))
	}
	var res struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Users []string `json:"users"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return plainTextResult("Could not parse the groups response: " + err.Error())
	}
	if len(res.Items) == 0 {
		return plainTextResult("No groups found.")
	}
	lines := make([]string, 0, len(res.Items))
	for _, g := range res.Items {
		if len(g.Users) == 0 {
			lines = append(lines, g.Metadata.Name+": (no users)")
		} else {
			lines = append(lines, g.Metadata.Name+": "+strings.Join(g.Users, ", "))
		}
	}
	return plainTextResult(strings.Join(lines, "\n"))
}

type listDeploymentsArgs struct {
	Token     string `json:"token"`
	Namespace string `json:"namespace,omitempty"`
}

// fetchDeployments returns "<ns>/<name> — <ready>/<replicas> ready" lines
// for one namespace.
func fetchDeployments(token, ns string) ([]string, int, string) {
	code, body, err := apiCall(http.MethodGet, "/apis/apps/v1/namespaces/"+ns+"/deployments", token, nil, "")
	if err != nil {
		return nil, 0, err.Error()
	}
	if code != http.StatusOK {
		return nil, code, statusMessage(body)
	}
	var res struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				Replicas *int `json:"replicas"`
			} `json:"spec"`
			Status struct {
				ReadyReplicas int `json:"readyReplicas"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, code, err.Error()
	}
	lines := make([]string, 0, len(res.Items))
	for _, d := range res.Items {
		replicas := 1
		if d.Spec.Replicas != nil {
			replicas = *d.Spec.Replicas
		}
		lines = append(lines, fmt.Sprintf("%s/%s — %d/%d ready", ns, d.Metadata.Name, d.Status.ReadyReplicas, replicas))
	}
	return lines, code, ""
}

func listDeployments(ctx context.Context, req *mcp.CallToolRequest, args listDeploymentsArgs) (*mcp.CallToolResult, any, error) {
	if msg := needToken(args.Token); msg != "" {
		return plainTextResult(msg)
	}
	if ns := strings.TrimSpace(args.Namespace); ns != "" {
		lines, code, msg := fetchDeployments(args.Token, ns)
		switch {
		case code == http.StatusForbidden:
			return plainTextResult(fmt.Sprintf("Not allowed: your account cannot list deployments in namespace '%s'.", ns))
		case code == http.StatusNotFound:
			return plainTextResult(fmt.Sprintf("Namespace '%s' was not found (or is not visible to your account).", ns))
		case code != http.StatusOK:
			return plainTextResult(fmt.Sprintf("Could not list deployments in namespace '%s' (HTTP %d): %s", ns, code, msg))
		case len(lines) == 0:
			return plainTextResult(fmt.Sprintf("No deployments in namespace '%s'.", ns))
		}
		return plainTextResult(strings.Join(lines, "\n"))
	}
	projects, code, msg := fetchProjects(args.Token)
	if code == http.StatusForbidden {
		return plainTextResult("Not allowed: your account cannot list namespaces/projects, so deployments cannot be listed.")
	}
	if code != http.StatusOK {
		return plainTextResult(fmt.Sprintf("Could not list namespaces (HTTP %d): %s", code, msg))
	}
	var all []string
	for _, ns := range projects {
		lines, dcode, _ := fetchDeployments(args.Token, ns)
		if dcode == http.StatusForbidden {
			all = append(all, fmt.Sprintf("%s: not allowed to list deployments in this namespace.", ns))
			continue
		}
		if dcode != http.StatusOK {
			continue
		}
		all = append(all, lines...)
	}
	if len(all) == 0 {
		return plainTextResult("No deployments found in any namespace you can see.")
	}
	return plainTextResult(strings.Join(all, "\n"))
}

type deploymentArgs struct {
	Token     string `json:"token"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

func restartDeployment(ctx context.Context, req *mcp.CallToolRequest, args deploymentArgs) (*mcp.CallToolResult, any, error) {
	if msg := needToken(args.Token); msg != "" {
		return plainTextResult(msg)
	}
	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`, time.Now().UTC().Format(time.RFC3339))
	path := fmt.Sprintf("/apis/apps/v1/namespaces/%s/deployments/%s", args.Namespace, args.Name)
	code, body, err := apiCall(http.MethodPatch, path, args.Token, []byte(patch), "application/strategic-merge-patch+json")
	if err != nil {
		return plainTextResult("Could not reach the cluster API: " + err.Error())
	}
	switch code {
	case http.StatusOK:
		return plainTextResult(fmt.Sprintf("Restarted deployment %s/%s.", args.Namespace, args.Name))
	case http.StatusForbidden:
		return plainTextResult(fmt.Sprintf("Not allowed: your account cannot restart deployment %s/%s (needs edit permissions in that namespace). %s", args.Namespace, args.Name, statusMessage(body)))
	case http.StatusNotFound:
		return plainTextResult(fmt.Sprintf("Not found: deployment %s/%s does not exist.", args.Namespace, args.Name))
	default:
		return plainTextResult(fmt.Sprintf("Could not restart deployment %s/%s (HTTP %d): %s", args.Namespace, args.Name, code, statusMessage(body)))
	}
}

// NOTE (2026-11-08): there is deliberately NO delete_deployment
// tool. Deleting a deployment removes the whole application — too
// powerful for this tool. The destructive action offered instead is
// delete_pods: pods are recreated by their deployment automatically.

type podList struct {
	Items []struct {
		Metadata struct {
			Name   string            `json:"name"`
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
		Status struct {
			Phase string `json:"phase"`
		} `json:"status"`
	} `json:"items"`
}

func fetchPods(token, ns string) ([]string, int, string) {
	code, body, err := apiCall(http.MethodGet, fmt.Sprintf("/api/v1/namespaces/%s/pods", ns), token, nil, "")
	if err != nil {
		return nil, 0, err.Error()
	}
	if code != http.StatusOK {
		return nil, code, statusMessage(body)
	}
	var pl podList
	if err := json.Unmarshal(body, &pl); err != nil {
		return nil, code, "could not parse the pod list: " + err.Error()
	}
	var lines []string
	for _, p := range pl.Items {
		line := fmt.Sprintf("%s/%s — %s", ns, p.Metadata.Name, p.Status.Phase)
		if app := p.Metadata.Labels["app"]; app != "" {
			line += fmt.Sprintf(" (deployment %s)", app)
		}
		lines = append(lines, line)
	}
	return lines, code, ""
}

type listPodsArgs struct {
	Token     string `json:"token"`
	Namespace string `json:"namespace"`
}

func listPods(ctx context.Context, req *mcp.CallToolRequest, args listPodsArgs) (*mcp.CallToolResult, any, error) {
	if msg := needToken(args.Token); msg != "" {
		return plainTextResult(msg)
	}
	if ns := strings.TrimSpace(args.Namespace); ns != "" {
		lines, code, msg := fetchPods(args.Token, ns)
		switch {
		case code == http.StatusForbidden:
			return plainTextResult(fmt.Sprintf("Not allowed: your account cannot list pods in namespace '%s'.", ns))
		case code == http.StatusNotFound:
			return plainTextResult(fmt.Sprintf("Namespace '%s' was not found (or is not visible to your account).", ns))
		case code != http.StatusOK:
			return plainTextResult(fmt.Sprintf("Could not list pods in namespace '%s' (HTTP %d): %s", ns, code, msg))
		case len(lines) == 0:
			return plainTextResult(fmt.Sprintf("No pods in namespace '%s'.", ns))
		}
		return plainTextResult(strings.Join(lines, "\n"))
	}
	projects, code, msg := fetchProjects(args.Token)
	if code != http.StatusOK {
		return plainTextResult(fmt.Sprintf("Could not list namespaces (HTTP %d): %s", code, msg))
	}
	var all []string
	for _, ns := range projects {
		lines, pcode, _ := fetchPods(args.Token, ns)
		if pcode != http.StatusOK {
			continue
		}
		all = append(all, lines...)
	}
	if len(all) == 0 {
		return plainTextResult("No pods found in any namespace you can see.")
	}
	return plainTextResult(strings.Join(all, "\n"))
}

type deletePodsArgs struct {
	Token     string `json:"token"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// deletePods deletes every pod of one deployment. The deployment itself
// is NOT touched — it recreates the pods right away.
func deletePods(ctx context.Context, req *mcp.CallToolRequest, args deletePodsArgs) (*mcp.CallToolResult, any, error) {
	if msg := needToken(args.Token); msg != "" {
		return plainTextResult(msg)
	}
	// Get the deployment only to read its pod selector labels.
	dpath := fmt.Sprintf("/apis/apps/v1/namespaces/%s/deployments/%s", args.Namespace, args.Name)
	code, body, err := apiCall(http.MethodGet, dpath, args.Token, nil, "")
	if err != nil {
		return plainTextResult("Could not reach the cluster API: " + err.Error())
	}
	switch code {
	case http.StatusForbidden:
		return plainTextResult(fmt.Sprintf("Not allowed: your account cannot see deployment %s/%s.", args.Namespace, args.Name))
	case http.StatusNotFound:
		return plainTextResult(fmt.Sprintf("Not found: deployment %s/%s does not exist.", args.Namespace, args.Name))
	case http.StatusOK:
	default:
		return plainTextResult(fmt.Sprintf("Could not read deployment %s/%s (HTTP %d): %s", args.Namespace, args.Name, code, statusMessage(body)))
	}
	var dep struct {
		Spec struct {
			Selector struct {
				MatchLabels map[string]string `json:"matchLabels"`
			} `json:"selector"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(body, &dep); err != nil || len(dep.Spec.Selector.MatchLabels) == 0 {
		return plainTextResult(fmt.Sprintf("Could not read the pod selector of deployment %s/%s.", args.Namespace, args.Name))
	}
	var pairs []string
	for k, v := range dep.Spec.Selector.MatchLabels {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	sel := url.QueryEscape(strings.Join(pairs, ","))
	code, body, err = apiCall(http.MethodGet, fmt.Sprintf("/api/v1/namespaces/%s/pods?labelSelector=%s", args.Namespace, sel), args.Token, nil, "")
	if err != nil {
		return plainTextResult("Could not reach the cluster API: " + err.Error())
	}
	if code != http.StatusOK {
		return plainTextResult(fmt.Sprintf("Could not list the pods of %s/%s (HTTP %d): %s", args.Namespace, args.Name, code, statusMessage(body)))
	}
	var pl podList
	if err := json.Unmarshal(body, &pl); err != nil {
		return plainTextResult("Could not parse the pod list: " + err.Error())
	}
	if len(pl.Items) == 0 {
		return plainTextResult(fmt.Sprintf("Deployment %s/%s has no pods right now.", args.Namespace, args.Name))
	}
	deleted := 0
	for _, p := range pl.Items {
		dcode, dbody, derr := apiCall(http.MethodDelete, fmt.Sprintf("/api/v1/namespaces/%s/pods/%s", args.Namespace, p.Metadata.Name), args.Token, nil, "")
		if derr != nil {
			return plainTextResult("Could not reach the cluster API: " + derr.Error())
		}
		if dcode == http.StatusForbidden {
			return plainTextResult(fmt.Sprintf("Not allowed: your account cannot delete pods in namespace '%s' (needs edit permissions there). %s", args.Namespace, statusMessage(dbody)))
		}
		if dcode != http.StatusOK {
			return plainTextResult(fmt.Sprintf("Could not delete pod %s (HTTP %d): %s", p.Metadata.Name, dcode, statusMessage(dbody)))
		}
		deleted++
	}
	return plainTextResult(fmt.Sprintf("Deleted %d pod(s) of deployment %s/%s. The deployment itself is untouched and will recreate the pods.", deleted, args.Namespace, args.Name))
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "openshift-mcp", Version: "2.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "list_clusters", Description: "List all known clusters: sandbox, dev, production."}, listClusters)
	mcp.AddTool(server, &mcp.Tool{Name: "connect_cluster", Description: "Connect (log in) to a cluster by name and report who we are there."}, connectCluster)
	mcp.AddTool(server, &mcp.Tool{Name: "login_url", Description: "Return the cluster's real login page (OAuth). The USER logs in there with their LDAP account; OpenShift gives permissions from their group."}, loginURL)
	mcp.AddTool(server, &mcp.Tool{Name: "cluster_status", Description: "Health/status summary for one cluster."}, clusterStatus)
	mcp.AddTool(server, &mcp.Tool{Name: "list_namespaces", Description: "List the namespaces (projects) visible to the signed-in user. Requires the user's OpenShift bearer token; results are limited by the user's own RBAC permissions."}, listNamespaces)
	mcp.AddTool(server, &mcp.Tool{Name: "list_projects", Description: "List the projects visible to the signed-in user. In OpenShift a project is a namespace with a display layer, so this returns the same set as list_namespaces; it exists as its own tool because users ask for 'projects'. Results are limited by the user's own RBAC permissions."}, listNamespaces)
	mcp.AddTool(server, &mcp.Tool{Name: "list_groups", Description: "List OpenShift groups and their users, using the signed-in user's token. Listing groups needs cluster permissions; a clear 'not allowed' text is returned otherwise."}, listGroups)
	mcp.AddTool(server, &mcp.Tool{Name: "list_deployments", Description: "List deployments ('<namespace>/<name> — <ready>/<replicas> ready') using the signed-in user's token. With no namespace, lists deployments across all namespaces the user can see."}, listDeployments)
	mcp.AddTool(server, &mcp.Tool{Name: "restart_deployment", Description: "Restart a deployment (rollout restart via the kubectl.kubernetes.io/restartedAt annotation) using the signed-in user's token; the user's edit permissions in that namespace decide if it works."}, restartDeployment)
	mcp.AddTool(server, &mcp.Tool{Name: "list_pods", Description: "List pods (in one namespace, or across all namespaces the user can see) using the signed-in user's token."}, listPods)
	mcp.AddTool(server, &mcp.Tool{Name: "delete_pods", Description: "Delete all pods of one deployment using the signed-in user's token. The deployment itself is NOT deleted; it recreates the pods. Needs the user's delete permission on pods in that namespace. There is intentionally no deployment-deletion tool."}, deletePods)

	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	log.Println("openshift-mcp (Go) listening on http://127.0.0.1:8000/mcp")
	log.Fatal(http.ListenAndServe("127.0.0.1:8000", mux))
}
