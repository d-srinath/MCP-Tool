# OpenLDAP (local POC identity)

Lightweight OpenLDAP on the CRC cluster = step 1 of docs/platform-plan.md.

- Base DN: `dc=platform,dc=local`
- Admin bind: `cn=admin,dc=platform,dc=local` (password in Secret `openldap-admin`, namespace `ldap`)
- OUs: `ou=people`, `ou=groups`
- Users: `srinath` (in admins), `dev1` (in developers). Dev password for both: `changeme123` (SSHA-hashed in the LDIF; local throwaway only)
- Groups: `admins`, `developers` (groupOfNames, `member` attribute — the shape OpenShift LDAP group sync expects)

Next half of step 1: add LDAP as an OpenShift OAuth identity provider,
sync groups, and bind RBAC (admins -> cluster-admin, developers -> edit on dev projects).

Deploy: `oc apply -f ldap/` then
`oc adm policy add-scc-to-user anyuid -z default -n ldap` (osixia image runs as root; local POC only).
