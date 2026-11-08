# MCP server + client — Go

Rewritten in Go (2026-11-08) at Srinath's direction — Go is the language
for app/cluster tooling in this repo; the original Python/FastMCP version
was a prototype shortcut and has been removed. Both programs use the
official Go MCP SDK (github.com/modelcontextprotocol/go-sdk).

- `../mcp-server/main.go` — MCP server (Go), streamable HTTP at
  http://127.0.0.1:8000/mcp. Tools: list_clusters, connect_cluster,
  cluster_status, login_url. Runs on the Mac (only place sandbox/CRC
  resolves).
- `../mcp-client/main.go` — Paas Portal chat UI (Go) at
  http://localhost:8090. The page heading is "Welcome to Paas Portal";
  the environments are not displayed upfront — they are listed only when
  asked. Every reply comes from a real MCP tool call; the client only
  routes the typed sentence to the right tool.

Clusters: sandbox = local CRC (live). dev / production = declared, not
configured — they connect in the organization phase.

Run on the Mac (Go installed via Homebrew: `brew install go`):
  cd mcp-server && go build -o /tmp/openshift-mcp . && /tmp/openshift-mcp &
  cd mcp-client && go build -o /tmp/paas-portal-client . && /tmp/paas-portal-client &
Then open http://localhost:8090 in Safari.
