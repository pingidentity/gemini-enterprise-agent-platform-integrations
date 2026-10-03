"""RFC 8693 token provider for Order Status Agent -> MCP calls.

Exchanges the gateway's delegated token (validated by auth.py) for an
MCP-scoped token: the delegated token as subject, this agent's own
client_credentials token as actor. The gateway extension remints that
result again for the MCP server — see the journey CLAUDE.md's token
exchange pattern.
"""

import threading
import time

import httpx

from config import (
    AGENT_CLIENT_ID,
    AGENT_CLIENT_SECRET,
    GATEWAY_AUDIENCE,
    TOKEN_ENDPOINT,
    TOOL_SCOPE,
)

_lock = threading.Lock()
_actor_token = ""
_actor_expires_at = 0.0
_exchange_cache: dict[str, tuple[str, float]] = {}


def _get_actor_token() -> str:
    """Return a cached client_credentials token for this agent, refreshing when near expiry."""
    global _actor_token, _actor_expires_at
    now = time.time()
    if _actor_token and now < _actor_expires_at:
        return _actor_token
    response = httpx.post(
        TOKEN_ENDPOINT,
        data={"grant_type": "client_credentials"},
        auth=(AGENT_CLIENT_ID, AGENT_CLIENT_SECRET),
        headers={"Content-Type": "application/x-www-form-urlencoded"},
        timeout=15,
    )
    response.raise_for_status()
    body = response.json()
    _actor_token = body["access_token"]
    _actor_expires_at = now + max(body.get("expires_in", 3600) - 30, 10)
    return _actor_token


def exchange_for_mcp(delegated_token: str) -> str:
    """Exchange the inbound delegated token for an MCP-scoped token."""
    if not delegated_token:
        raise ValueError("inbound delegated token is required")
    with _lock:
        now = time.time()
        cached = _exchange_cache.get(delegated_token)
        if cached and now < cached[1]:
            return cached[0]
        response = httpx.post(
            TOKEN_ENDPOINT,
            data={
                "grant_type": "urn:ietf:params:oauth:grant-type:token-exchange",
                "subject_token": delegated_token,
                "subject_token_type": "urn:ietf:params:oauth:token-type:access_token",
                "actor_token": _get_actor_token(),
                "actor_token_type": "urn:ietf:params:oauth:token-type:access_token",
                "requested_token_type": "urn:ietf:params:oauth:token-type:access_token",
                "audience": GATEWAY_AUDIENCE,
                "scope": TOOL_SCOPE,
            },
            auth=(AGENT_CLIENT_ID, AGENT_CLIENT_SECRET),
            timeout=15,
        )
        response.raise_for_status()
        body = response.json()
        token = body["access_token"]
        _exchange_cache[delegated_token] = (token, now + max(body.get("expires_in", 300) - 30, 10))
        return token
