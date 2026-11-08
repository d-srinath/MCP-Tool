# MCP server + client — Go

Rewritten in Go (2026-11-08) by project decision — Go is the language
for app/cluster tooling in this repo; the original Python/FastMCP version
was a prototype shortcut and has been removed. Both programs use the
official Go MCP SDK (github.com/modelcontextprotocol/go-sdk).

- `../mcp-server/main.go` — MCP server (Go), streamable HTTP at
  http://127.0.0.1:8000/mcp. Runs on the Mac (only place sandbox/CRC
  resolves). See the tool list below.
- `../mcp-client/main.go` — Paas Portal chat UI (Go) at
  http://localhost:8090. The page heading is "Welcome to Paas Portal";
  the environments are not displayed upfront — they are listed only when
  asked. Every reply comes from a real MCP tool call; the client only
  routes the typed sentence to the right tool. See
  `../mcp-client/README.md` for the sign-in flow.

Clusters: sandbox = local CRC (live). dev / production = declared, not
configured — they connect in the organization phase.

## Tools

v1 (unchanged, JSON results):

- `list_clusters` — sandbox, dev, production.
- `connect_cluster` — local status of the sandbox connection.
- `cluster_status` — CRC status summary for one cluster.
- `login_url` — the cluster's real (console) login URL + provider note.

v2 — work tools (plain-text results). Each takes a required `token`
argument: the signed-in user's own OpenShift bearer token. The server
calls https://api.crc.testing:6443 with `Authorization: Bearer <token>`,
so the user's LDAP group / RBAC permissions decide everything:

- `list_namespaces(token)` — projects the user can see, one per line.
- `list_groups(token)` — each group with its users. Listing groups needs
  cluster permissions; without them the tool returns a clear
  "Not allowed: ..." text instead of failing.
- `list_deployments(token, namespace?)` — `<namespace>/<name> —
  <ready>/<replicas> ready` lines; with no namespace, across all
  namespaces the user can see.
- `restart_deployment(token, namespace, name)` — rollout restart via
  the `kubectl.kubernetes.io/restartedAt` annotation (strategic merge
  patch).
- `list_pods(token, namespace?)` — pod name/phase lines; with no
  namespace, across all namespaces the user can see.
- `delete_pods(token, namespace, name)` — deletes all pods of the named
  deployment. The deployment itself is NOT deleted (it recreates the
  pods). There is deliberately **no** `delete_deployment` tool
  (2026-11-08): deleting a deployment removes the whole
  application — too powerful for this tool.

403/404 responses become clear "Not allowed: ..." / "Not found: ..."
texts including the API's own Status message where present.

Note: the API calls skip TLS verification because local CRC serves a
self-signed certificate. The organization version must use the
cluster's real CA.

Run on the Mac (Go installed via Homebrew: `brew install go`):
  cd mcp-server && go build -o /tmp/openshift-mcp . && /tmp/openshift-mcp &
  cd mcp-client && go build -o /tmp/paas-portal-client . && /tmp/paas-portal-client &
Then open http://localhost:8090 in Safari.
