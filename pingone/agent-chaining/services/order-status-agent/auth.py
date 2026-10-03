"""Delegated-token validation — the agent's inbound security boundary.

Validates the gateway-reminted token (carried in the A2A message metadata)
before it is used as an RFC 8693 subject_token: signature via JWKS, issuer,
audience (Order Status Agent's own PingOne resource), `sub` presence, scope,
and the `act` chain.

The gateway extension already validates the token it mints before injecting
it; this is an independent re-check before the token is spent, matching the
defense-in-depth pattern every other hop in this journey uses (the extension
validates, then the target it forwards to validates again).
"""

from typing import Any

import httpx
from jose import JWTError, jwk, jwt

from config import EXPECTED_AUDIENCE, EXPECTED_SCOPE, IDP_ISSUER

JWKS_URL = f"{IDP_ISSUER}/jwks"

_jwks: dict[str, Any] | None = None


def _jwks_keys() -> dict[str, Any]:
    global _jwks
    if _jwks is None:
        response = httpx.get(JWKS_URL, timeout=10)
        response.raise_for_status()
        _jwks = response.json()
    return _jwks


def validate_inbound_token(token: str) -> dict[str, Any]:
    """Validate the gateway's delegated token before using it as a subject token."""
    if not token:
        raise ValueError("a signed PingOne delegated token is required")
    try:
        header = jwt.get_unverified_header(token)
        key_data = next(
            key for key in _jwks_keys().get("keys", []) if key.get("kid") == header.get("kid")
        )
        # PingOne JWKS omits the "alg" field; infer it from kty.
        algorithm = header.get("alg") or ("ES256" if key_data.get("kty") == "EC" else "RS256")
        claims = jwt.decode(
            token,
            jwk.construct(key_data, algorithm=algorithm),
            algorithms=[algorithm],
            issuer=IDP_ISSUER,
            audience=EXPECTED_AUDIENCE,
        )
    except (JWTError, StopIteration, KeyError, ValueError) as exc:
        raise ValueError("invalid inbound delegated token") from exc
    if not claims.get("sub"):
        raise ValueError("inbound delegated token is missing sub")
    scopes = set(str(claims.get("scope", "")).split())
    if EXPECTED_SCOPE not in scopes:
        raise ValueError("inbound delegated token scope mismatch")
    return claims


def act_chain(claims: dict[str, Any]) -> str:
    """Flatten the nested `act` claim into `sub1 -> sub2 -> ...` for logging."""
    act = claims.get("act") or {}
    parts = []
    while isinstance(act, dict) and act.get("sub"):
        parts.append(act["sub"])
        act = act.get("act")
    return " -> ".join(parts) or "<none>"
