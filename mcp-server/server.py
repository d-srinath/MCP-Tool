"""OpenShift MCP server (v1).

Exposes cluster tools over MCP (streamable HTTP on 127.0.0.1:8000/mcp).
Runs ON Srinath's Mac, where the local CRC cluster (sandbox) resolves.
Clusters 'dev' and 'production' are declared but not configured yet --
they arrive in the organization phase. Tools answer honestly about that.
"""
import json
import subprocess

from fastmcp import FastMCP

mcp = FastMCP("openshift-mcp")

CLUSTERS = {
    "sandbox": {
        "name": "sandbox",
        "description": "Local OpenShift CRC cluster on Srinath's MacBook",
        "kind": "local-crc",
        "configured": True,
    },
    "dev": {
        "name": "dev",
        "description": "Organization dev cluster (not connected yet)",
        "kind": "organization",
        "configured": False,
    },
    "production": {
        "name": "production",
        "description": "Organization production cluster (not connected yet)",
        "kind": "organization",
        "configured": False,
    },
}


def _oc(script: str) -> str:
    out = subprocess.run(
        ["/bin/zsh", "-lc", f"eval $(crc oc-env); {script}"],
        capture_output=True, text=True, timeout=60,
    )
    return (out.stdout or "").strip()


def _sandbox_connect() -> dict:
    whoami = _oc("oc whoami")
    server = _oc("oc whoami --show-server")
    node = _oc("oc get nodes --no-headers | head -1")
    version = _oc("crc status | grep -i openshift") if whoami else ""
    return {
        "cluster": "sandbox",
        "connected": bool(whoami),
        "user": whoami,
        "server": server,
        "openshift": version,
        "node": node,
    }


@mcp.tool
def list_clusters() -> str:
    """List all known clusters: sandbox, dev, production."""
    return json.dumps(list(CLUSTERS.values()))


@mcp.tool
def connect_cluster(name: str) -> str:
    """Connect (log in) to a cluster by name and report who we are there."""
    key = name.strip().lower()
    if key == "prod":
        key = "production"
    info = CLUSTERS.get(key)
    if info is None:
        return json.dumps({"connected": False, "error": f"unknown cluster '{name}'"})
    if not info["configured"]:
        return json.dumps({
            "cluster": key,
            "connected": False,
            "reason": "This cluster is not configured yet. It will be connected in the organization phase.",
        })
    return json.dumps(_sandbox_connect())


@mcp.tool
def login_url(name: str) -> str:
    """Return the cluster's real login page (OAuth). The USER logs in there
    with their LDAP account; OpenShift gives permissions from their group."""
    key = name.strip().lower()
    if key == "prod":
        key = "production"
    if key in ("local", "crc", "local openshift"):
        key = "sandbox"
    info = CLUSTERS.get(key)
    if info is None:
        return json.dumps({"error": f"unknown cluster '{name}'"})
    if not info["configured"]:
        return json.dumps({
            "cluster": key,
            "configured": False,
            "reason": "This cluster is not configured yet, so it has no login page yet. It arrives in the organization phase.",
        })
    return json.dumps({
        "cluster": key,
        "configured": True,
        "url": "https://oauth-openshift.apps-crc.testing/oauth/authorize?client_id=console&response_type=code&scope=user:full&redirect_uri=https%3A%2F%2Fconsole-openshift-console.apps-crc.testing%2Fauth%2Fcallback",
        "provider": "ldap",
        "note": "Log in choosing the 'ldap' provider (e.g. srinath-admin or srinath-developer). Permissions come from your LDAP group: admins = cluster-admin, developers = edit in the dev project only.",
    })


@mcp.tool
def cluster_status(name: str) -> str:
    """Health/status summary for one cluster."""
    key = name.strip().lower()
    if key == "prod":
        key = "production"
    info = CLUSTERS.get(key)
    if info is None:
        return json.dumps({"error": f"unknown cluster '{name}'"})
    if not info["configured"]:
        return json.dumps({"cluster": key, "status": "not configured yet"})
    status_text = _oc("crc status")
    projects = _oc("oc get projects --no-headers | wc -l")
    return json.dumps({
        "cluster": key,
        "status": status_text,
        "projects_visible": projects,
    })


if __name__ == "__main__":
    mcp.run(transport="http", host="127.0.0.1", port=8000)
