# Paas Portal — MCP client UI (Go)

Chat UI for the OpenShift MCP server, at http://localhost:8090.
Heading: "Welcome to Paas Portal". The clusters are NOT shown upfront —
they are listed only when the user asks (e.g. "list clusters").

## The v2 flow (per-user sign-in)

1. The user asks "what are all the clusters" → the portal lists
   sandbox, dev, production (via the MCP `list_clusters` tool).
2. "Connect me to sandbox" → if not signed in, the reply carries a
   **Sign in to sandbox** button pointing at `/auth/login`.
3. `/auth/login` redirects to the OpenShift OAuth authorize page
   (`client_id=paas-portal`, scope `user:full`). The user logs in on
   the real OpenShift login page, choosing the **ldap** provider with
   their own account.
4. OpenShift redirects back to `/auth/callback?code=...`. The portal
   exchanges the code for the user's access token, reads the username
   from `GET /apis/user.openshift.io/v1/users/~`, and stores
   {token, username} **server-side, in memory only**, keyed by a random
   session id in an HttpOnly `paas_session` cookie. The token never
   reaches the browser.
5. Back in the chat the user issues work commands — "list namespaces",
   "list groups", "list deployments", "list pods in Y",
   "restart deployment X in Y", "delete pods of X in Y". The portal
   passes the session token to the MCP server's v2 tools, so every
   action runs with exactly the user's LDAP/RBAC permissions
   (admin = everything; developers = view in `dev` plus their assigned
   testing project). Deployments are never deleted by design
   (2026-11-08) — asking to delete one gets an explanation
   and the pod-deletion alternative; only pods can be deleted, and
   the deployment recreates them.
6. The header shows the signed-in username next to the "MCP Paas Tool"
   badge, with a **Sign out** link (`/auth/logout` — local sign-out
   only; it drops the portal session, not the OpenShift SSO session).

Connecting to dev / production still answers "not configured yet" —
those clusters arrive in the organization phase.

## The OAuth secret

The OAuthClient `paas-portal` is registered on the cluster by
`../manifests/oauthclient-paas-portal.yaml` (its `secret:` value there
is a placeholder). The real secret was generated on the cluster at
install time and is **never in git**. At runtime the portal reads it
from, in order:

1. the `PAAS_OAUTH_SECRET` environment variable, or
2. the file `~/.paas-portal/oauth-secret`.

## Commands the chat understands

- `list clusters` / `what are all the clusters`
- `connect to sandbox` / `connect to local` / `log in into dev cluster`
- `status of sandbox cluster`
- (signed in) `list namespaces`, `list groups`, `list deployments`,
  `list deployments in <namespace>`
- (signed in) `restart deployment <name> in <namespace>`
- (signed in) `list pods`, `list pods in <namespace>`
- (signed in) `delete pods of <name> in <namespace>` (pods only —
  deployments are never deleted; asking to delete a deployment returns
  an explanation)

Run (with the MCP server already running on :8000):
  cd mcp-client && go build -o /tmp/paas-portal-client . && /tmp/paas-portal-client &
Then open http://localhost:8090.
