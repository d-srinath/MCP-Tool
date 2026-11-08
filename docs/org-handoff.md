# Organization handoff

Everything for the local platform lives in this one repo. To take it to the
organization: clone this repo into the organization's Git system, then work
through the swap list below. Nothing else is needed from the local setup.

## What is in this repo
- `app/`, `manifests/`, `scripts/` — the original POC workload (poc-web).
- `ldap/` — OpenLDAP manifests, bootstrap LDIF (users/groups), the OAuth
  identity-provider record, group-sync config, RBAC notes.
- `mcp-server/` — FastMCP server (list_clusters / connect_cluster /
  cluster_status / login_url tools).
- `mcp-client/` — chat UI: cluster cards (sandbox / dev / production) and
  commands ("list clusters", "connect to sandbox" -> real login page).
- `docs/platform-plan.md` — the A-to-Z plan and setup order.
- `docs/org-handoff.md` — this file.

## Swap list for the organization
1. Clusters: point the MCP server's cluster registry at the real dev and
   production clusters (API URLs + login routes). Sandbox can stay as the
   local CRC entry.
2. Identity: replace the local OpenLDAP with the organization's LDAP/AD as
   the OpenShift identity provider; keep the same pattern — groups in the
   directory, sync into OpenShift, permissions only via group RBAC
   (admins -> cluster-admin is a LOCAL choice; in the org, bind the roles
   the org approves, in non-prod namespaces first).
3. Credentials: none of the local dev passwords (changeme123 etc.) may be
   reused. Org secrets come from the org's vault/process.
4. Jenkins / Argo CD: use the org instances if they exist; the Jenkinsfile
   and the Argo CD Application in this repo are the patterns to copy.
5. Access: Muse (or any automation) works under a named person's identity
   and org policy — no shared kubeadmin.
