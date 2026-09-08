"""Support Agent — delegates order questions to the native Order Status A2A agent."""

from __future__ import annotations

import json
import os
from typing import Any
from uuid import uuid4

import httpx
from google.adk.agents import Agent
from google.adk.tools import FunctionTool
from google.adk.tools.tool_context import ToolContext
from google.genai import types as genai_types
from jose import JWTError, jwk, jwt

from pingone import get_delegated_token

ORDER_STATUS_AGENT_URL = os.environ["A2A_ORDER_STATUS_AGENT_URL"]

# Inbound validation of the browser's own PKCE login token — audienced to
# this agent's own PingOne resource (see CLAUDE.md's PingOne setup section).
# Agent Bridge already validates this token before storing it in session
# state; this is an independent re-check, matching the defense-in-depth
# pattern every other hop in this journey uses (the extension validates,
# then the target it forwards to validates again).
EXPECTED_AUDIENCE = os.environ.get("SUPPORT_AGENT_AUDIENCE", "support-agent")
EXPECTED_SCOPE = os.environ.get("SUPPORT_AGENT_EXPECTED_SCOPE", "support-agent:invoke")
ISSUER = os.environ.get("AGENT_IDP_TOKEN_ENDPOINT", "").removesuffix("/token").rstrip("/")
JWKS_URL = f"{ISSUER}/jwks" if ISSUER else ""

_jwks: dict[str, Any] | None = None


def _jwks_keys() -> dict[str, Any]:
    global _jwks
    if _jwks is None:
        response = httpx.get(JWKS_URL, timeout=10)
        response.raise_for_status()
        _jwks = response.json()
    return _jwks


def _validate_user_token(token: str) -> dict[str, Any]:
    """Validate the browser's PingOne login token before using it as a subject token."""
    if not token:
        raise ValueError("user token is required")
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
        raise ValueError("invalid user token") from exc
    if not claims.get("sub"):
        raise ValueError("user token is missing sub")
    scopes = set(str(claims.get("scope", "")).split())
    if EXPECTED_SCOPE not in scopes:
        raise ValueError("user token scope mismatch")
    return claims


def get_order_status(order_id: str, tool_context: ToolContext) -> dict:
    """Delegate an order-status request to the native A2A agent."""
    print("[support-agent] get_order_status start order_id=" + order_id, flush=True)
    user_token = tool_context.state.get("user_token")
    if not user_token:
        raise RuntimeError("authenticated user token is missing from session state")
    if not order_id.startswith("ORD-") or not order_id[4:].isdigit():
        return {
            "error": "Invalid order ID. Please use the format ORD-123, for example ORD-123 or ORD-456."
        }

    try:
        _validate_user_token(user_token)
    except ValueError as exc:
        print(f"[support-agent] inbound token rejected: {exc}", flush=True)
        return {"error": f"unauthorized: {exc}"}

    try:
        delegated = get_delegated_token(user_token)
        request_id = str(uuid4())
        response = httpx.post(
            f"{ORDER_STATUS_AGENT_URL}/message:send",
            json={
                "message": {
                    "messageId": request_id,
                    "role": "ROLE_USER",
                    "parts": [{"text": f"get_order_status:{order_id}"}],
                },
                # The gateway extension remints this into its own delegated
                # token before forwarding; sent here so the request is
                # well-formed even if the gateway hop is ever bypassed.
                "metadata": {"delegatedAuthorization": f"Bearer {delegated}"},
            },
            headers={
                "Authorization": f"Bearer {delegated}",
                "A2A-Version": "1.0",
            },
            timeout=30,
        )
        response.raise_for_status()
        payload = response.json()
    except Exception as exc:
        print(f"[support-agent] A2A call failed: {type(exc).__name__}: {exc}", flush=True)
        return {"error": f"order status lookup failed: {exc}"}
    message = payload.get("message") or (payload.get("task") or {}).get("status", {}).get("message")
    if not message:
        return payload
    texts = [part.get("text") for part in message.get("parts", []) if part.get("text")]
    return {"response": "\n".join(texts) if texts else json.dumps(message)}


root_agent = Agent(
    model="gemini-2.5-flash",
    name="support_agent",
    description="Support agent that delegates order-status requests to a specialized agent.",
    instruction=(
        "You are a customer support agent. When a user asks about an order, extract the order ID and call get_order_status. "
        "Order IDs must use the format ORD-123, such as ORD-123 or ORD-456. "
        "If the user gives an invalid or incomplete ID, explain the required format instead of calling the tool. "
        "Report the result clearly. Do not access order data directly; the Order Status Agent owns that capability."
    ),
    tools=[FunctionTool(get_order_status)],
    # Thinking (thought_signature) on function-call turns intermittently
    # crashed the deployed engine's VertexAiSessionService persistence with
    # zero events returned to the client. Disabled to keep tool calls reliable.
    generate_content_config=genai_types.GenerateContentConfig(
        thinking_config=genai_types.ThinkingConfig(thinking_budget=0),
    ),
)
