# Organization handoff — Paas Portal + OpenShift MCP server

What this is: a chat portal (the MCP client) and an MCP server that let a
signed-in user ask for OpenShift work in plain words ("list the projects",
"restart deployment app1 in mcp-testing-1"). Everything runs **as the
user**, with the user's own OpenShift token — the tools have no
credentials of their own.

Verified against the code on 2026-11-08 (Go implementation; the earlier
Python/FastMCP version mentioned in previous revisions is gone).

## How the sign-in works

1. The user types "connect me to dev" (or sandbox / production) in the
   portal chat.
2. The portal answers **in the chat** with a "Sign in" button. Nothing
   navigates until the user clicks it.
3. The button sends the browser to that cluster's OpenShift OAuth
   authorize URL. The cluster's OAuth is backed by the organization's
   LDAP/AD, so the user logs in with their normal org account and picks
   the LDAP provider.
4. The cluster redirects back to the portal's `/auth/callback` with an
   authorization code. The portal exchanges the code for the **user's
   access token** (using the `paas-portal` OAuthClient and its secret),
   looks up the username, and stores {token, username} server-side,
   keyed by an HttpOnly session cookie. The token never reaches the
   browser's page content.
5. Every later chat command calls an MCP tool with that token; the MCP
   server calls the cluster API with it. The cluster's own RBAC decides
   what succeeds — admins see/do everything their group allows,
   developers only what they are granted. Signing out drops the portal
   session (the cluster SSO session is separate).

## Step 1 — Copy these, unchanged

- `mcp-server/` (Go MCP server, tools on :8000/mcp)
- `mcp-client/` (Go Paas Portal chat UI + OAuth sign-in, :8090)
- `manifests/oauthclient-paas-portal.yaml` (the OAuthClient to register
  on each cluster — placeholder secret, replaced per cluster)
- This doc.

Do **not** copy into the org: `ldap/` (the local OpenLDAP, test users
testadmin1-3 / testdeveloper1-3 and the local group bindings), the local
kubeconfig/CRC settings, or any local password. The org uses its own
directory and RBAC.

## Step 2 — Update these exact spots

In `mcp-client/main.go` (top constants):
- `oauthAuthorizeURL`, `oauthTokenURL` — currently
  `https://oauth-openshift.apps-crc.testing/...`; set to the target
  cluster's `https://oauth-openshift.<cluster-domain>/...`.
- `apiBaseURL` — currently `https://api.crc.testing:6443`.
- `redirectURI` — currently `http://localhost:8090/auth/callback`; set
  to the portal's real org URL + `/auth/callback`, and register exactly
  this URI in the cluster's OAuthClient.
- The TLS config marked "local CRC self-signed cert only"
  (`InsecureSkipVerify: true`) — replace with the cluster's real CA.
- `mcpURL` — where the portal reaches the MCP server, if not co-located.
- Secret: provide the cluster's `paas-portal` OAuthClient secret via
  env `PAAS_OAUTH_SECRET` (or `~/.paas-portal/oauth-secret`). A fresh
  secret per cluster; never reuse the local one.

In `mcp-server/main.go`:
- `apiBase` — currently `https://api.crc.testing:6443`; set per cluster.
- The cluster registry (`clusters` list, top of file) — sandbox is the
  local CRC entry; dev and production are placeholders with
  `Configured: false`. Point them at the real org clusters (name,
  description, API/OAuth endpoints) and mark them configured.
- `login_url` result's console URL — currently the CRC console host.
- Same TLS change as the client (real CA, no InsecureSkipVerify).

## Step 3 — On each org cluster (cluster-admin work, org side)

1. Make sure the cluster's OAuth is wired to the org LDAP/AD and groups
   are synced; confirm the org's RBAC bindings for those groups. (The
   local demo binds admins → cluster-admin and developers → view in
   `dev` — that is a local choice; the org applies its own policy.)
2. Apply the `paas-portal` OAuthClient with that cluster's generated
   secret and the portal's redirect URI.
3. Deploy/run the MCP server where it can reach the cluster API, and
   the portal where users can reach it.

## Notes / limits to know before the org pilot

- Sign-in is per cluster: the portal's OAuth constants above target one
  cluster's OAuth server. Multi-cluster sign-in (a separate token per
  cluster in one portal session) is the next iteration — today, point
  one portal instance at one cluster, or extend the constants to a
  per-cluster table.
- Portal sessions are in-memory: restarting the portal signs everyone
  out. Fine for a pilot; add a session store for production.
- By design there is **no deployment-deletion tool** (Srinath,
  2026-11-08). The destructive action offered is deleting pods, which
  the deployment recreates. Tools: list_clusters, connect_cluster,
  login_url, cluster_status, list_namespaces, list_projects,
  list_groups, list_deployments, list_pods, restart_deployment,
  delete_pods.
- The rest of this repo (app/, scripts/, Jenkins/Argo CD patterns) is
  the broader platform POC — see `docs/platform-plan.md`. Access rule
  carried into the org: automation works under a named person's
  identity and org policy — no shared kubeadmin.
