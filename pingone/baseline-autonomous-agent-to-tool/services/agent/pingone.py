"""PingOne client_credentials token provider for the agent's MCP requests."""

import os
import threading
import time

import httpx

_TOKEN_ENDPOINT = os.environ.get("AGENT_IDP_TOKEN_ENDPOINT", "")
_CLIENT_ID = os.environ.get("AGENT_IDP_CLIENT_ID", "")
_CLIENT_SECRET = os.environ.get("AGENT_IDP_CLIENT_SECRET", "")
_SCOPE = os.environ.get("AGENT_IDP_SCOPE", "")

_lock = threading.Lock()
_cached_token = ""
_expires_at = 0.0


def _fetch_token() -> str:
    data = {"grant_type": "client_credentials"}
    if _SCOPE:
        data["scope"] = _SCOPE

    resp = httpx.post(
        _TOKEN_ENDPOINT,
        data=data,
        auth=(_CLIENT_ID, _CLIENT_SECRET),
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
    headers = {
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream",
    }
    if _TOKEN_ENDPOINT and _CLIENT_ID and _CLIENT_SECRET:
        headers["Authorization"] = f"Bearer {get_token()}"
    return headers
