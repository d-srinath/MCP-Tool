# MCP server + client (v1)

- `../mcp-server/server.py` — FastMCP server, streamable HTTP at
  http://127.0.0.1:8000/mcp. Tools: list_clusters, connect_cluster,
  cluster_status. Runs on the Mac (only place sandbox/CRC resolves).
- `../mcp-client/app.py` — chat UI at http://localhost:8090 with cluster
  cards (sandbox / dev / production). Every reply comes from a real MCP
  tool call; the client only routes the typed sentence to the right tool.

Clusters: sandbox = local CRC (live). dev / production = declared, not
configured — they connect in the organization phase.

Run on the Mac:
  cd mcp-server && uv venv --python 3.12 && uv pip install fastmcp && nohup .venv/bin/python server.py &
  cd mcp-client && uv venv --python 3.12 && uv pip install fastmcp && nohup .venv/bin/python app.py &
