# Local OpenShift Platform — A-to-Z Plan

Goal: replicate a real enterprise OpenShift workflow on the local CRC cluster first,
prove it end to end, then take the same pattern to the organization.
The MCP tool sits on top so an AI can operate this stack.

## The flow we are building
Developer/AI -> MCP tools -> Jenkins (CI) builds & tests, pushes image to the
internal registry, and commits the new image tag to Git -> Argo CD (GitOps)
detects the Git change and deploys/syncs it into OpenShift -> LDAP groups
(admin / developer) control who can do what at every step.

## A-to-Z components

### Done
1. CRC cluster (OpenShift 4.21.8) + oc + kubeadmin — Muse access via Tailscale + SSH. DONE
2. GitLab project (openshift-poc) + SSH key. DONE

### To set up, in order
3. Identity — OpenLDAP server (lightweight, running on the cluster):
   - Users and groups: `admins`, `developers`
   - OpenShift LDAP identity provider login wired to it
   - Group sync into OpenShift + RBAC: admins -> cluster-admin,
     developers -> edit on their own projects only
4. Namespaces / environments: `dev` (later `stage`), resource quotas and limits.
5. Registry: OpenShift internal registry (already in CRC) for built images.
6. Argo CD (OpenShift GitOps operator): an Application watching the manifests
   in this GitLab repo; auto-sync on change; Argo CD owns deployment from there.
7. Jenkins (CI) on OpenShift: pipeline = checkout -> build -> test -> build
   container image -> push to internal registry -> update image tag in this
   repo and commit. That commit is what Argo CD deploys. Jenkins builds,
   Argo CD deploys — Jenkins does not deploy directly.
   (Note: OpenShift Pipelines/Tekton is the native alternative; we use Jenkins
   because that is the tool in the real day-to-day workflow.)
8. MCP server (Python + FastMCP, runs on the Mac — Mac needs Python 3.12 via uv;
   system Python is 3.9): tools for an AI to operate the stack —
   list/get projects & pods, read logs, rollout status, scale,
   trigger a Jenkins pipeline, read Argo CD sync/health status.
   Tested with a small MCP client script against CRC.
9. Observability basics: OpenShift monitoring + pod/log views already in CRC;
   extend later (logging stack) as needed.
10. Secrets: plain OpenShift secrets locally; Vault/sealed-secrets is an org-phase item.

### Org phase (later, separate checklist)
Org cluster access (VPN/SSO oc login), org LDAP/AD instead of local OpenLDAP,
non-prod namespace first, scoped service accounts instead of kubeadmin,
org GitLab/Jenkins/Argo CD instances, approvals per org policy.

## First passenger
The existing poc-web app in this repo will be the first workload run through
the Jenkins -> Git -> Argo CD flow end to end.
