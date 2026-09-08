"""Native Agent Runtime A2A Order Status Agent."""

from __future__ import annotations

import base64
import json
import os
import time
from typing import Any
from uuid import uuid4

import httpx
from a2a.helpers.proto_helpers import new_text_message
from a2a.server.agent_execution import AgentExecutor, RequestContext
from a2a.server.events.event_queue_v2 import EventQueue
from a2a.types import AgentSkill
from jose import JWTError, jwk, jwt
from agentplatform.agent_engines.templates.a2a import A2aAgent, create_agent_card

from pingone import exchange_for_mcp

MCP_URL = os.environ["MCP_ORDER_STATUS_SERVER_URL"]
EXPECTED_AUDIENCE = os.environ.get("A2A_ORDER_STATUS_AUDIENCE", "order-status-agent")
EXPECTED_SCOPE = os.environ.get("A2A_ORDER_STATUS_SCOPE", "order-status:invoke")
TOKEN_ENDPOINT = os.environ["AGENT_IDP_TOKEN_ENDPOINT"]
ISSUER = os.environ.get(
    "IDP_ISSUER",
    TOKEN_ENDPOINT.removesuffix("/token").rstrip("/"),
).rstrip("/")
JWKS_URL = f"{ISSUER}/jwks"

_jwks: dict[str, Any] | None = None


def _jwks_keys() -> dict[str, Any]:
    global _jwks
    if _jwks is None:
        response = httpx.get(JWKS_URL, timeout=10)
        response.raise_for_status()
        _jwks = response.json()
    return _jwks


def _validate_inbound_token(token: str) -> dict[str, Any]:
    """Validate the Support Agent delegation before using it as a subject token."""
    if not token or token.startswith("local-rfc8693."):
        raise ValueError("a signed PingOne delegated token is required")
    try:
        header = jwt.get_unverified_header(token)
        key_data = next(
            key for key in _jwks_keys().get("keys", []) if key.get("kid") == header.get("kid")
        )
        algorithm = header.get("alg") or ("ES256" if key_data.get("kty") == "EC" else "RS256")
        claims = jwt.decode(
            token,
            jwk.construct(key_data, algorithm=algorithm),
            algorithms=[algorithm],
            issuer=ISSUER,
            audience=EXPECTED_AUDIENCE,
        )
    except (JWTError, StopIteration, KeyError, ValueError) as exc:
        raise ValueError("invalid inbound delegated token") from exc

    if not claims.get("sub"):
        raise ValueError("inbound delegated token is missing sub")
    scopes = set(str(claims.get("scope", "")).split())
    if EXPECTED_SCOPE not in scopes:
        raise ValueError("inbound delegated token scope mismatch")
    # Log the verified delegation: who the call is for (sub, the user
    # throughout), which audience it was minted for (aud), who acted for it
    # (act — the chain of agents that touched this token), and the scope.
    act = claims.get("act") or {}
    act_parts = []
    while isinstance(act, dict) and act.get("sub"):
        act_parts.append(act["sub"])
        act = act.get("act")
    print(
        "[OrderStatusAgent] Delegated token verified — sub={} aud={} act={}"
        " scope=\"{}\"".format(
            claims["sub"], claims.get("aud"), " -> ".join(act_parts) or "<none>",
            claims.get("scope", ""),
        ),
        flush=True,
    )
    return claims


def _order_id_from_message(context: RequestContext) -> str:
    text = context.get_user_input().strip()
    action, separator, order_id = text.partition(":")
    if action != "get_order_status" or not separator or not order_id.startswith("ORD-"):
        raise ValueError("expected get_order_status:<order_id>")
    if not order_id[4:].isdigit():
        raise ValueError("invalid order id")
    return order_id


def _authorization_token(context: RequestContext) -> str:
    # Authorization carries a Google credential the gateway injects to pass
    # this native A2A endpoint's own Google IAM check. A custom header
    # carrying the PingOne delegated bearer was observed not to survive the
    # hop from gateway to this container, so it travels in the request
    # metadata (part of the A2A message body) instead.
    authorization = str(context.metadata.get("delegatedAuthorization", ""))
    prefix, _, token = authorization.partition(" ")
    if prefix.lower() != "bearer" or not token:
        raise ValueError("inbound delegated bearer token is required")
    return token


def _call_order_mcp(order_id: str, token: str) -> dict[str, Any]:
    print("[OrderStatusAgent] get_order_status start order_id=" + order_id, flush=True)
    downstream_token = exchange_for_mcp(token)
    print("[OrderStatusAgent] exchanged for MCP token (aud=order-status-mcp-server), calling MCP server order_id=" + order_id, flush=True)
    response = httpx.post(
        MCP_URL,
        json={
            "jsonrpc": "2.0",
            "id": str(uuid4()),
            "method": "tools/call",
            "params": {"name": "get_order_status", "arguments": {"order_id": order_id}},
        },
        headers={"Authorization": f"Bearer {downstream_token}"},
        timeout=15,
    )
    response.raise_for_status()
    return response.json()


class OrderStatusExecutor(AgentExecutor):
    """Executes the one supported A2A action without exposing auth to the model."""

    async def execute(self, context: RequestContext, event_queue: EventQueue) -> None:
        order_id = _order_id_from_message(context)
        inbound_token = _authorization_token(context)
        claims = _validate_inbound_token(inbound_token)
        result = _call_order_mcp(order_id, inbound_token)
        print("[OrderStatusAgent] get_order_status complete order_id={} status={}".format(
            order_id, result.get("result", {}).get("structuredContent", {}).get("status", "<unknown>")
        ), flush=True)
        await event_queue.enqueue_event(new_text_message(json.dumps(result)))

    async def cancel(self, context: RequestContext, event_queue: EventQueue) -> None:
        await event_queue.enqueue_event(new_text_message("order-status request cancelled"))


skill = AgentSkill(
    id="get_order_status",
    name="Get order status",
    description="Retrieve the current status of an order.",
    tags=["orders", "status"],
    examples=["get_order_status:ORD-123"],
)

agent_card = create_agent_card(
    agent_name="Order Status Agent",
    description="Specialized agent that retrieves order status through a protected MCP server.",
    skills=[skill],
    default_input_modes=["text/plain"],
    default_output_modes=["application/json"],
    streaming=False,
)

root_agent = A2aAgent(
    agent_card=agent_card,
    agent_executor_builder=OrderStatusExecutor,
)
