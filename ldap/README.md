# OpenLDAP (local POC identity)

Lightweight OpenLDAP on the CRC cluster = step 1 of docs/platform-plan.md.

- Base DN: `dc=platform,dc=local`
- Admin bind: `cn=admin,dc=platform,dc=local` (password in Secret `openldap-admin`, namespace `ldap`)
- OUs: `ou=people`, `ou=groups`
- Users: `testadmin1..3` (in admins), `testdeveloper1..3` (in developers). Password for all: `changeme123` (SSHA-hashed in the LDIF; local throwaway only)
- Groups: `admins`, `developers` (groupOfNames, `member` attribute — the shape OpenShift LDAP group sync expects)

Next half of step 1: add LDAP as an OpenShift OAuth identity provider,
sync groups, and bind RBAC (admins -> cluster-admin, developers -> view on the dev project).

Deploy: `oc apply -f ldap/` then
`oc adm policy add-scc-to-user anyuid -z default -n ldap` (osixia image runs as root; local POC only).

## Update 2026-11-08 — step 1b done
LDAP is an OpenShift OAuth identity provider (idp-oauth.yaml), groups are
synced (sync-config.yaml), and RBAC is bound: admins = cluster-admin,
developers = view on project `dev` only (changed from edit on 2026-11-08 —
see the update note below).

## Update 2026-11-08 — generic test users replace personal accounts
Browser LDAP login was proven end to end in a clean (incognito) browser
context: choosing the `ldap` provider and logging in lands in the restricted
developer view. All personal-named accounts (`srinath`, `srinath-admin`,
`srinath-developer`, `dev1`) were then DELETED from LDAP and from OpenShift,
and replaced with generic org-style test accounts (test-users.ldif /
test-groups.ldif; bootstrap ConfigMap updated to match): testadmin1..3 in
admins, testdeveloper1..3 in developers. Group sync re-run; CLI login tests:
testdeveloper1 sees only project `dev` and is Forbidden from nodes,
testadmin1 sees the node Ready.

## Update 2026-11-08 — permission model set by Srinath
Admin level = everything, developer level = view only. The developers
group binding in project `dev` was changed from `edit` to `view`
(`oc adm policy remove-role-from-group edit developers -n dev`,
`oc adm policy add-role-to-group view developers -n dev`). Verified with
testdeveloper1's own token: get deployments/pods in `dev` = yes,
patch/delete = no, and an MCP restart attempt returns "Not allowed".
Admins keep cluster-admin. This is a LOCAL demo policy; the organization
uses its own RBAC (see docs/org-handoff.md).
