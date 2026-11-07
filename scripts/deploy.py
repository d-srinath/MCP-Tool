#!/usr/bin/env python3
"""Deploy the Muse OpenShift POC to a CRC cluster.

Run ON the Mac (or any host whose kubeconfig points at CRC), because
api.crc.testing only resolves there.

Usage:
    eval $(crc oc-env)
    python3 scripts/deploy.py [--project muse-poc] [--replicas 2]
"""
import argparse, subprocess, sys, time

def run(cmd, check=True):
    print("+", " ".join(cmd))
    r = subprocess.run(cmd, text=True, capture_output=True)
    if r.stdout: print(r.stdout, end="")
    if r.stderr: print(r.stderr, end="", file=sys.stderr)
    if check and r.returncode != 0:
        sys.exit(f"FAILED: {' '.join(cmd)}")
    return r

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--project", default="muse-poc")
    ap.add_argument("--replicas", default="2")
    a = ap.parse_args()
    run(["oc", "new-project", a.project], check=False)
    run(["oc", "project", a.project])
    for f in ["namespace.yaml", "configmap.yaml", "deployment.yaml", "service.yaml", "route.yaml"]:
        if f == "namespace.yaml":
            continue  # project already created above
        run(["oc", "apply", "-f", f"manifests/{f}"])
    run(["oc", "scale", "deployment/poc-web", f"--replicas={a.replicas}"])
    run(["oc", "rollout", "status", "deployment/poc-web", "--timeout=180s"])
    run(["oc", "get", "pods", "-l", "app=poc-web"])
    host = run(["oc", "get", "route", "poc-web", "-o", "jsonpath={.spec.host}"]).stdout.strip()
    print(f"App URL: http://{host}/")
    out = run(["curl", "-s", "-m", "20", "-w", "\nHTTP_STATUS:%{http_code}\n", f"http://{host}/"])
    assert "Muse OpenShift POC works" in out.stdout and "HTTP_STATUS:200" in out.stdout, "verification failed"
    print("POC verification PASSED")

if __name__ == "__main__":
    main()
