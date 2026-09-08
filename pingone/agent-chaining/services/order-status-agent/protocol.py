"""Shared request and local RFC 8693-shaped token helpers.

Production deployments replace the local token factory with a real PingOne
RFC 8693 exchange. The local token payload deliberately mirrors the claims the
exchange produces: sub, aud, scope, and act.
"""

from __future__ import annotations

import base64
from dataclasses import dataclass
import json
import os
import re
import time
from typing import Any
from uuid import uuid4

A2A_METHOD = "message/send"
ORDER_STATUS_ACTION = "get_order_status"
ORDER_STATUS_TOOL = "get_order_status"
SUPPORT_AGENT = "support-agent"
ORDER_STATUS_AGENT = "order-status-agent"
ORDER_STATUS_MCP_SERVER = "order-status-mcp-server"
ORDER_ID_PATTERN = re.compile(r"ORD-[0-9]+")


@dataclass(frozen=True)
class OrderStatusRequest:
    order_id: str


@dataclass(frozen=True)
class DelegatedToken:
    subject: str
    audience: str
    scope: str
    actor: str
    token_id: str
    expires_at: int


def validate_order_id(order_id: str) -> str:
    if not isinstance(order_id, str) or not ORDER_ID_PATTERN.fullmatch(order_id):
        raise ValueError("invalid order id")
    return order_id


def build_a2a_request(order_id: str, request_id: str) -> dict[str, Any]:
    validate_order_id(order_id)
    if not request_id:
        raise ValueError("request id is required")
    return {
        "jsonrpc": "2.0",
        "id": request_id,
        "method": A2A_METHOD,
        "params": {
            "message": {
                "messageId": request_id,
                "parts": [{"kind": "text", "text": f"{ORDER_STATUS_ACTION}:{order_id}"}],
            }
        },
    }


def parse_order_status_request(body: dict[str, Any]) -> OrderStatusRequest:
    if not isinstance(body, dict) or body.get("jsonrpc") != "2.0" or body.get("method") != A2A_METHOD:
        raise ValueError("expected JSON-RPC message/send")
    try:
        message = body["params"]["message"]
        if message["messageId"] != body["id"]:
            raise ValueError("message id does not match JSON-RPC id")
        text = next(part["text"] for part in message["parts"] if part.get("kind") == "text")
    except (KeyError, TypeError, StopIteration) as exc:
        raise ValueError("message/send requires a text message part") from exc
    action, separator, order_id = text.partition(":")
    if action != ORDER_STATUS_ACTION or not separator:
        raise ValueError("expected get_order_status:<order_id>")
    validate_order_id(order_id)
    return OrderStatusRequest(order_id=order_id)


def _encode(payload: dict[str, Any]) -> str:
    raw = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode()
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode()


def _decode(value: str) -> dict[str, Any]:
    raw = base64.urlsafe_b64decode(value + "=" * (-len(value) % 4))
    return json.loads(raw)


def create_local_delegated_token(*, subject: str, audience: str, scope: str, actor: str, expires_in: int = 60) -> str:
    """Model an RFC 8693 result for local development only."""
    if os.getenv("LOCAL_DELEGATION_MODE", "true").lower() != "true":
        raise RuntimeError("local token mode is disabled; configure a real token exchange")
    payload = {
        "sub": subject,
        "aud": audience,
        "scope": scope,
        "act": {"sub": actor},
        "jti": str(uuid4()),
        "exp": int(time.time()) + expires_in,
    }
    return "local-rfc8693." + _encode(payload)


def parse_delegated_token(token: str, *, audience: str, scope: str, actor: str | None = None) -> DelegatedToken:
    """Validate the local RFC 8693-shaped token at a receiving boundary."""
    try:
        prefix, encoded = token.split(".", 1)
        if prefix != "local-rfc8693":
            raise ValueError
        payload = _decode(encoded)
        subject = payload["sub"]
        actual_audience = payload["aud"]
        actual_scope = payload["scope"]
        actual_actor = payload["act"]["sub"]
        expires_at = int(payload["exp"])
        token_id = payload["jti"]
    except (ValueError, KeyError, TypeError, json.JSONDecodeError, UnicodeDecodeError) as exc:
        raise ValueError("invalid delegated token") from exc
    if actual_audience != audience:
        raise ValueError("delegated token audience mismatch")
    if scope not in actual_scope.split():
        raise ValueError("delegated token scope mismatch")
    if actor is not None and actual_actor != actor:
        raise ValueError("delegated token actor mismatch")
    if expires_at <= int(time.time()):
        raise ValueError("delegated token expired")
    return DelegatedToken(subject, actual_audience, actual_scope, actual_actor, token_id, expires_at)


def bearer_token(authorization: str) -> str:
    prefix, _, token = authorization.partition(" ")
    if prefix != "Bearer" or not token:
        raise ValueError("bearer token required")
    return token
