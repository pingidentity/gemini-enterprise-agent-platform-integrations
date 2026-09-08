"""RFC 8693 token provider for Support Agent -> Order Status Agent calls."""

import base64
import json
import os
import threading
import time
from uuid import uuid4

import httpx

_TOKEN_ENDPOINT = os.environ.get("AGENT_IDP_TOKEN_ENDPOINT", "")
_CLIENT_ID = os.environ.get("AGENT_IDP_CLIENT_ID", "")
_CLIENT_SECRET = os.environ.get("AGENT_IDP_CLIENT_SECRET", "")
_SCOPE = os.environ.get("A2A_ORDER_STATUS_SCOPE", "order-status:invoke")
_ACTOR_SCOPE = os.environ.get("AGENT_IDP_SCOPE", "").strip()
# Targets the shared intermediate "agent-gateway" audience, not the real
# order-status-agent audience directly — the gateway extension performs the
# real exchange to order-status-agent on top of this one. See the gateway
# extension's .env.sample for why (keeps order-status-agent's PingOne
# resource purely terminal instead of being touched by two exchanges).
_AUDIENCE = os.environ.get("AGENT_GATEWAY_AUDIENCE", "ac-google-cloud-agent-gateway")

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
        _TOKEN_ENDPOINT,
        data={"grant_type": "client_credentials", **({"scope": _ACTOR_SCOPE} if _ACTOR_SCOPE else {})},
        auth=(_CLIENT_ID, _CLIENT_SECRET),
        headers={"Content-Type": "application/x-www-form-urlencoded"},
        timeout=15,
    )
    response.raise_for_status()
    body = response.json()
    _actor_token = body["access_token"]
    _actor_expires_at = now + max(body.get("expires_in", 3600) - 30, 10)
    return _actor_token


def _local_token(subject: str) -> str:
    payload = {
        "sub": subject,
        "aud": _AUDIENCE,
        "scope": _SCOPE,
        "act": {"sub": os.environ.get("SUPPORT_AGENT_ID", "support-agent")},
        "jti": str(uuid4()),
        "exp": int(time.time()) + 60,
    }
    encoded = base64.urlsafe_b64encode(json.dumps(payload).encode()).rstrip(b"=").decode()
    return "local-rfc8693." + encoded


def get_delegated_token(user_token: str) -> str:
    """Exchange the user token for an A2A token; local mode models the claims."""
    if not user_token:
        raise ValueError("user token is required")
    if os.environ.get("LOCAL_DELEGATION_MODE", "true").lower() == "true":
        return _local_token(user_token.removeprefix("local-user:") or "local-user")

    with _lock:
        now = time.time()
        cached = _delegated_cache.get(user_token)
        if cached and now < cached[1]:
            return cached[0]
        actor = _get_actor_token()
        response = httpx.post(
            _TOKEN_ENDPOINT,
            data={
                "grant_type": "urn:ietf:params:oauth:grant-type:token-exchange",
                "subject_token": user_token,
                "subject_token_type": "urn:ietf:params:oauth:token-type:access_token",
                "actor_token": actor,
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
        _delegated_cache[user_token] = (token, now + max(body.get("expires_in", 300) - 30, 10))
        return token
