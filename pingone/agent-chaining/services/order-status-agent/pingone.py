"""RFC 8693 token provider for Order Status Agent -> MCP calls."""

import os
import threading
import time

import httpx

_ENDPOINT = os.environ["AGENT_IDP_TOKEN_ENDPOINT"]
_CLIENT_ID = os.environ["AGENT_IDP_CLIENT_ID"]
_CLIENT_SECRET = os.environ["AGENT_IDP_CLIENT_SECRET"]
# Targets the shared intermediate "agent-gateway" audience, not the real
# order-status-mcp-server audience directly — the gateway extension performs
# the real exchange to order-status-mcp-server on top of this one. See the
# gateway extension's .env.sample for why (keeps order-status-mcp-server's
# PingOne resource purely terminal instead of being touched by two exchanges).
_AUDIENCE = os.environ.get("AGENT_GATEWAY_AUDIENCE", "ac-google-cloud-agent-gateway")
_SCOPE = os.environ["MCP_ORDER_STATUS_SCOPE"]

_lock = threading.Lock()
_actor_token = ""
_actor_expires_at = 0.0
_exchange_cache: dict[str, tuple[str, float]] = {}


def _get_actor_token() -> str:
    global _actor_token, _actor_expires_at
    now = time.time()
    if _actor_token and now < _actor_expires_at:
        return _actor_token
    response = httpx.post(
        _ENDPOINT,
        data={"grant_type": "client_credentials"},
        auth=(_CLIENT_ID, _CLIENT_SECRET),
        headers={"Content-Type": "application/x-www-form-urlencoded"},
        timeout=15,
    )
    response.raise_for_status()
    body = response.json()
    _actor_token = body["access_token"]
    _actor_expires_at = now + max(body.get("expires_in", 3600) - 30, 10)
    return _actor_token


def exchange_for_mcp(user_delegated_token: str) -> str:
    """Exchange the inbound delegated token for an MCP-scoped token."""
    if not user_delegated_token:
        raise ValueError("inbound delegated token is required")
    with _lock:
        now = time.time()
        cached = _exchange_cache.get(user_delegated_token)
        if cached and now < cached[1]:
            return cached[0]
        response = httpx.post(
            _ENDPOINT,
            data={
                "grant_type": "urn:ietf:params:oauth:grant-type:token-exchange",
                "subject_token": user_delegated_token,
                "subject_token_type": "urn:ietf:params:oauth:token-type:access_token",
                "actor_token": _get_actor_token(),
                "actor_token_type": "urn:ietf:params:oauth:token-type:access_token",
                "requested_token_type": "urn:ietf:params:oauth:token-type:access_token",
                "audience": _AUDIENCE,
                "scope": _SCOPE,
            },
            auth=(_CLIENT_ID, _CLIENT_SECRET),
            timeout=15,
        )
        response.raise_for_status()
        body = response.json()
        token = body["access_token"]
        _exchange_cache[user_delegated_token] = (token, now + max(body.get("expires_in", 300) - 30, 10))
        return token
