"""Shared order-request helpers (A2A wire-shape constants and parsing)."""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Any

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


def bearer_token(authorization: str) -> str:
    prefix, _, token = authorization.partition(" ")
    if prefix != "Bearer" or not token:
        raise ValueError("bearer token required")
    return token
