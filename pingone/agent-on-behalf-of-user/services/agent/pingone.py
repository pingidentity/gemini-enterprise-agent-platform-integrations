"""PingOne RFC 8693 token provider for the OBO agent's MCP requests.

The agent bridge validates the user's token and stores it in ADK session state
under "user_token". This module reads that token, exchanges it for a delegated
token (sub=user, act.client_id=agent) using the agent's own PingOne credentials,
and attaches the result to every outbound MCP request.
"""

import logging
import os
import threading
import time

import httpx

_TOKEN_ENDPOINT = os.environ.get("AGENT_IDP_TOKEN_ENDPOINT", "")
_CLIENT_ID = os.environ.get("AGENT_IDP_CLIENT_ID", "")
_CLIENT_SECRET = os.environ.get("AGENT_IDP_CLIENT_SECRET", "")
_SCOPE = os.environ.get("AGENT_IDP_SCOPE", "")

_lock = threading.Lock()
_actor_token = ""
_actor_expires_at = 0.0
# Maps subject user_token → (delegated_token, expires_at)
_delegated_cache: dict[str, tuple[str, float]] = {}


def _get_actor_token() -> str:
    """Return a cached client_credentials token for this agent, refreshing when near expiry."""
    global _actor_token, _actor_expires_at
    now = time.time()
    if _actor_token and now < _actor_expires_at:
        return _actor_token
    resp = httpx.post(
        _TOKEN_ENDPOINT,
        data={"grant_type": "client_credentials"},
        auth=(_CLIENT_ID, _CLIENT_SECRET),
        headers={"Content-Type": "application/x-www-form-urlencoded"},
        timeout=15,
    )
    resp.raise_for_status()
    body = resp.json()
    _actor_token = body["access_token"]
    _actor_expires_at = now + max(body.get("expires_in", 3600) - 30, 10)
    return _actor_token


def _exchange(user_token: str) -> tuple[str, int]:
    """RFC 8693: exchange user token (subject) + agent token (actor) → delegated token."""
    actor = _get_actor_token()
    data: dict[str, str] = {
        "grant_type": "urn:ietf:params:oauth:grant-type:token-exchange",
        "subject_token": user_token,
        "subject_token_type": "urn:ietf:params:oauth:token-type:access_token",
        "actor_token": actor,
        "actor_token_type": "urn:ietf:params:oauth:token-type:access_token",
        "requested_token_type": "urn:ietf:params:oauth:token-type:access_token",
    }
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
        raise RuntimeError(f"no access_token in exchange response: {body}")
    logging.info("delegated token minted (ttl %ss)", body.get("expires_in", 300))
    return token, body.get("expires_in", 300)


def get_delegated_token(user_token: str) -> str:
    """Return a cached delegated token for user_token, exchanging when near expiry."""
    with _lock:
        now = time.time()
        cached = _delegated_cache.get(user_token)
        if cached:
            tok, exp = cached
            if now < exp:
                return tok
        tok, expires_in = _exchange(user_token)
        _delegated_cache[user_token] = (tok, now + max(expires_in - 30, 10))
        return tok


def mcp_headers(ctx) -> dict[str, str]:
    """ADK header_provider: attach a delegated PingOne token to outbound MCP requests.

    During tool discovery (ctx.state has no user_token), falls back to the
    agent's own client_credentials token so the gateway accepts the request
    and ADK can register the tool list.
    """
    headers = {
        "Content-Type": "application/json",
        "Accept": "application/json, text/event-stream",
    }
    if not (_TOKEN_ENDPOINT and _CLIENT_ID and _CLIENT_SECRET):
        return headers

    user_token = ctx.state.get("user_token", "") if ctx and hasattr(ctx, "state") else ""

    try:
        if user_token:
            # Normal request: exchange for a delegated token on behalf of the user.
            token = get_delegated_token(user_token)
        else:
            # Tool discovery (no user context yet): use the agent's own token.
            token = _get_actor_token()
        headers["Authorization"] = f"Bearer {token}"
    except Exception as exc:
        logging.warning("Failed to get MCP auth token: %s", exc)

    return headers
