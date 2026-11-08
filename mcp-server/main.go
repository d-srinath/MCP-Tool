// OpenShift MCP server (v1) — Go implementation.
//
// Exposes cluster tools over MCP (streamable HTTP on 127.0.0.1:8000/mcp).
// Runs ON Srinath's Mac, where the local CRC cluster (sandbox) resolves.
// Clusters 'dev' and 'production' are declared but not configured yet --
// they arrive in the organization phase. Tools answer honestly about that.
//
// This is a port of the original Python/FastMCP server.py: same tools,
// same names, same inputs, same JSON outputs.
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os/exec"
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
		Note:       "Log in choosing the 'ldap' provider (e.g. srinath-admin or srinath-developer). Permissions come from your LDAP group: admins = cluster-admin, developers = edit in the dev project only.",
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

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "openshift-mcp", Version: "1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "list_clusters", Description: "List all known clusters: sandbox, dev, production."}, listClusters)
	mcp.AddTool(server, &mcp.Tool{Name: "connect_cluster", Description: "Connect (log in) to a cluster by name and report who we are there."}, connectCluster)
	mcp.AddTool(server, &mcp.Tool{Name: "login_url", Description: "Return the cluster's real login page (OAuth). The USER logs in there with their LDAP account; OpenShift gives permissions from their group."}, loginURL)
	mcp.AddTool(server, &mcp.Tool{Name: "cluster_status", Description: "Health/status summary for one cluster."}, clusterStatus)

	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	log.Println("openshift-mcp (Go) listening on http://127.0.0.1:8000/mcp")
	log.Fatal(http.ListenAndServe("127.0.0.1:8000", mux))
}
