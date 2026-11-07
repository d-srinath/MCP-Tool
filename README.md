# OpenShift POC — local first, then organization

Owner: Srinath Damacharla. Built/tested by Muse on Srinath's MacBook CRC cluster.

## Purpose
Prove the workflow locally on OpenShift Local (CRC) first — Muse accesses the
MacBook over Tailscale + SSH and runs `oc` on the Mac, because
`api.crc.testing` only resolves on the Mac itself. Once the local proof works,
the same code and manifests are taken to the organization cluster.

## Languages
- **Go** — primary language for deployable apps / cluster tooling (planned next app).
- **Python** — automation scripts (`scripts/`), already available on the Mac.

## What is here
- `app/index.html` — the POC web page (source of the ConfigMap content).
- `manifests/` — clean, re-appliable manifests: namespace, configmap, deployment, service, route.
  - Deployment: `poc-web`, image `openshift/httpd:2.4-ubi9` from the internal registry,
    ConfigMap mounted at `/var/www/html`, 2 replicas.
  - `manifests/live-export/` — raw `oc get -o yaml` exports from the live cluster (2026-11-07).
- `scripts/deploy.py` — applies the manifests, scales, waits for rollout, and verifies the route.

## Verified result (2026-11-07, local CRC 4.21.8)
- Project `muse-poc`, deployment `poc-web`: 2/2 pods Running
- Route `poc-web-muse-poc.apps-crc.testing` returned HTTP 200 with "Muse OpenShift POC works"
- Scaled 1 -> 2 replicas successfully

## Deploy
On the Mac:
```sh
eval $(crc oc-env)
python3 scripts/deploy.py
```

## Rules
- Every code/manifest update is committed here and pushed to Srinath's GitLab
  (standing instruction, 2026-11-07).
- MacBook must be awake with the lid open and Tailscale connected for Muse to reach it.
- Org phase: no kubeadmin — use Srinath's org user and a non-prod namespace.
