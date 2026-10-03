"""RFC 8693 token provider for Support Agent -> Order Status Agent calls."""

import threading
import time

import httpx

from config import AGENT_CLIENT_ID, AGENT_CLIENT_SECRET, AGENT_SCOPE, GATEWAY_AUDIENCE, TOKEN_ENDPOINT

_lock = threading.Lock()
_actor_token = ""
_actor_expires_at = 0.0
_delegated_cache: dict[str, tuple[str, float]] = {}


def _get_actor_token() -> str:
    """Return this agent's cached client-credentials token."""
    global _actor_token, _actor_expires_at
    now = time.time()
    if _actor_token and now < _actor_expires_at:
        return _actor_token
    response = httpx.post(
        TOKEN_ENDPOINT,
        data={"grant_type": "client_credentials", "scope": AGENT_SCOPE},
        auth=(AGENT_CLIENT_ID, AGENT_CLIENT_SECRET),
        headers={"Content-Type": "application/x-www-form-urlencoded"},
        timeout=15,
    )
    response.raise_for_status()
    body = response.json()
    _actor_token = body["access_token"]
    _actor_expires_at = now + max(body.get("expires_in", 3600) - 30, 10)
    return _actor_token


def get_delegated_token(user_token: str) -> str:
    """Exchange the user token for an A2A token via PingOne RFC 8693."""
    if not user_token:
        raise ValueError("user token is required")

    with _lock:
        now = time.time()
        cached = _delegated_cache.get(user_token)
        if cached and now < cached[1]:
            return cached[0]
        actor = _get_actor_token()
        response = httpx.post(
            TOKEN_ENDPOINT,
            data={
                "grant_type": "urn:ietf:params:oauth:grant-type:token-exchange",
                "subject_token": user_token,
                "subject_token_type": "urn:ietf:params:oauth:token-type:access_token",
                "actor_token": actor,
                "actor_token_type": "urn:ietf:params:oauth:token-type:access_token",
                "requested_token_type": "urn:ietf:params:oauth:token-type:access_token",
                "audience": GATEWAY_AUDIENCE,
                "scope": AGENT_SCOPE,
            },
            auth=(AGENT_CLIENT_ID, AGENT_CLIENT_SECRET),
            timeout=15,
        )
        response.raise_for_status()
        body = response.json()
        token = body["access_token"]
        _delegated_cache[user_token] = (token, now + max(body.get("expires_in", 300) - 30, 10))
        return token
