"""MCP client UI (v1) -- chat page + cluster cards.

Stdlib web server on 127.0.0.1:8090. Every answer comes from calling the
MCP server (http://127.0.0.1:8000/mcp) with the official FastMCP client;
this app only routes the typed command to the right MCP tool.
"""
import asyncio
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from fastmcp import Client

MCP_URL = "http://127.0.0.1:8000/mcp"
CLUSTER_NAMES = ["sandbox", "dev", "production"]


async def call_tool(name: str, args: dict) -> str:
    async with Client(MCP_URL) as client:
        result = await client.call_tool(name, args)
        return result.content[0].text


def find_cluster(text: str):
    t = text.lower()
    if "sandbox" in t or "local" in t or "crc" in t:
        return "sandbox"
    if "production" in t or "prod" in t:
        return "production"
    if "dev" in t:
        return "dev"
    return None


def route(message: str):
    text = message.lower()
    cluster = find_cluster(text)
    try:
        if "list" in text or "what are" in text or "which cluster" in text or "all the clusters" in text:
            data = json.loads(asyncio.run(call_tool("list_clusters", {})))
            lines = ["Here are all the clusters I know:"] + [
                f"• {c['name']} — {c['description']} ({'ready' if c['configured'] else 'not configured yet'})"
                for c in data
            ]
            return {"reply": "\n".join(lines)}
        if "connect" in text or "log in" in text or "login" in text:
            if not cluster:
                return {"reply": "Which cluster — sandbox, dev, or production? Try: 'connect to sandbox'."}
            data = json.loads(asyncio.run(call_tool("login_url", {"name": cluster})))
            if data.get("configured"):
                return {
                    "reply": (f"Opening the OpenShift login page for {cluster}. "
                              f"Choose the 'ldap' provider and sign in with your own account "
                              f"(srinath-admin or srinath-developer, dev password changeme123). "
                              f"OpenShift will give you exactly your group's permissions. "
                              f"If the console opens without asking, log out (top-right) first, then log in again and pick 'ldap'."),
                    "action_url": data["url"],
                    "action_label": "Open the OpenShift login page",
                }
            return {"reply": f"Cannot connect to {cluster} yet: {data.get('reason') or data.get('error')}"}
        if "status" in text or "health" in text:
            target = cluster or "sandbox"
            data = json.loads(asyncio.run(call_tool("cluster_status", {"name": target})))
            if "status" in data:
                return {"reply": f"Status of {target}:\n{data['status']}\nProjects visible: {data.get('projects_visible', '?')}"}
            return {"reply": f"{target}: {data.get('error', 'unknown')}"}
        return {"reply": ("I can do these today:\n"
                "• 'list clusters' / 'what are all the clusters'\n"
                "• 'connect to sandbox' or 'connect to local' — opens the real OpenShift (LDAP) login page\n"
                "• 'status of sandbox cluster'")}
    except Exception as e:
        return {"reply": f"Something went wrong talking to the MCP server: {e}"}


HTML = """<!doctype html><html><head><meta charset="utf-8"><title>OpenShift MCP Client</title>
<style>
body{font-family:-apple-system,Helvetica,Arial,sans-serif;margin:0;background:#f4f5f7;color:#1b1e23}
header{background:#15181d;color:#fff;padding:18px 28px} header h1{margin:0;font-size:20px} header p{margin:4px 0 0;color:#9ba1a9;font-size:13px}
.wrap{max-width:880px;margin:24px auto;padding:0 16px}
.cards{display:flex;gap:14px;flex-wrap:wrap;margin-bottom:20px}
.card{background:#fff;border-radius:10px;padding:16px 18px;flex:1;min-width:220px;box-shadow:0 1px 3px rgba(0,0,0,.08)}
.card h3{margin:0 0 6px;text-transform:capitalize}
.chip{display:inline-block;padding:2px 9px;border-radius:20px;font-size:12px;background:#e8f5e9;color:#1e7d32}
.chip.off{background:#f1f1f1;color:#777}
.card small{color:#666;display:block;margin-top:6px}
#chat{background:#fff;border-radius:10px;box-shadow:0 1px 3px rgba(0,0,0,.08);padding:18px}
#log{height:300px;overflow:auto;display:flex;flex-direction:column;gap:10px;margin-bottom:12px}
.msg{padding:10px 13px;border-radius:10px;white-space:pre-wrap;max-width:85%;font-size:14px;line-height:1.45}
.me{align-self:flex-end;background:#dbeafe}
.bot{align-self:flex-start;background:#f1f1f1}
form{display:flex;gap:8px} input{flex:1;padding:11px;border:1px solid #ccc;border-radius:8px;font-size:14px}
button{padding:11px 18px;border:0;border-radius:8px;background:#c00;color:#fff;font-size:14px;cursor:pointer}
</style></head><body>
<header><h1>OpenShift MCP Client</h1><p>Talk to your clusters. Answers come from the MCP server's tools.</p></header>
<div class="wrap">
<div class="cards" id="cards"></div>
<div id="chat"><div id="log"></div>
<form id="f"><input id="i" placeholder="Try: list clusters — or — connect me to sandbox cluster" autocomplete="off"><button>Send</button></form>
</div></div>
<script>
async function loadCards(){
  const r = await fetch('/api/clusters'); const data = await r.json();
  document.getElementById('cards').innerHTML = data.map(c =>
    `<div class="card"><h3>${c.name}</h3><span class="chip ${c.configured?'':'off'}">${c.configured?'ready':'not configured yet'}</span><small>${c.description}</small></div>`).join('');
}
function add(text, cls){ const d=document.createElement('div'); d.className='msg '+cls; d.textContent=text; const log=document.getElementById('log'); log.appendChild(d); log.scrollTop=log.scrollHeight; }
document.getElementById('f').addEventListener('submit', async e => {
  e.preventDefault(); const inp=document.getElementById('i'); const text=inp.value.trim(); if(!text) return;
  add(text,'me'); inp.value=''; add('Working on it…','bot'); const pending=document.querySelector('#log .bot:last-child');
  const r = await fetch('/api/chat', {method:'POST', headers:{'Content-Type':'application/json'}, body: JSON.stringify({message:text})});
  const data = await r.json(); pending.textContent = data.reply;
  if (data.action_url){ window.open(data.action_url, '_blank'); const a=document.createElement('a'); a.href=data.action_url; a.target='_blank'; a.textContent=data.action_label||'Open'; a.style.cssText='display:inline-block;margin-top:8px;padding:8px 14px;background:#c00;color:#fff;border-radius:8px;text-decoration:none'; pending.appendChild(document.createElement('br')); pending.appendChild(a); }
});
loadCards(); add('Hello Srinath. Ask me to list the clusters, or say: connect me to sandbox cluster.','bot');
</script></body></html>"""


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def _json(self, obj, code=200):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/":
            body = HTML.encode()
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        elif self.path == "/api/clusters":
            try:
                data = json.loads(asyncio.run(call_tool("list_clusters", {})))
                self._json(data)
            except Exception as e:
                self._json({"error": str(e)}, 500)
        else:
            self._json({"error": "not found"}, 404)

    def do_POST(self):
        if self.path != "/api/chat":
            self._json({"error": "not found"}, 404)
            return
        length = int(self.headers.get("Content-Length", "0"))
        payload = json.loads(self.rfile.read(length) or b"{}")
        self._json(route(payload.get("message", "")))


if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", 8090), Handler).serve_forever()
