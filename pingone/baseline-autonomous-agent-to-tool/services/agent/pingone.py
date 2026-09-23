"""Mints the agent's PingOne identity token and attaches it to every MCP request.

The agent authenticates as itself (client_credentials, no user context — this is
the autonomous-agent journey). The gateway's extension service validates this
token, asks PingOne Authorize for a PERMIT/DENY, then exchanges it for a
tool-scoped token before the request reaches the MCP tool. Caching: one token,
refreshed 30s before expiry, guarded by a lock because ADK runs concurrent
async tool calls.
"""

import threading
import time
import httpx
from config import AGENT_CLIENT_ID, AGENT_CLIENT_SECRET, TOKEN_ENDPOINT, TOOL_SCOPE

_lock = threading.Lock()
_cached_token = ""
_expires_at = 0.0


def _fetch_token() -> str:
    resp = httpx.post(
        TOKEN_ENDPOINT,
        data={"grant_type": "client_credentials", "scope": TOOL_SCOPE},
        auth=(AGENT_CLIENT_ID, AGENT_CLIENT_SECRET),
        headers={"Content-Type": "application/x-www-form-urlencoded"},
        timeout=15,
    )
    resp.raise_for_status()
    body = resp.json()
    token = body.get("access_token", "")
    if not token:
        raise RuntimeError(f"no access_token in PingOne response: {body}")

    global _cached_token, _expires_at
    # Refresh 30s early; never cache for less than 10s.
    ttl = max(body.get("expires_in", 3600) - 30, 10)
    _cached_token = token
    _expires_at = time.time() + ttl
    return token


def get_token() -> str:
    """Return a cached PingOne access token, refreshing when near expiry."""
    with _lock:
        if _cached_token and time.time() < _expires_at:
            return _cached_token
        return _fetch_token()


def mcp_headers(_ctx) -> dict[str, str]:
    """ADK header_provider: attach the agent's PingOne token to MCP requests."""
    return {
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream",
        "Authorization": f"Bearer {get_token()}",
    }
