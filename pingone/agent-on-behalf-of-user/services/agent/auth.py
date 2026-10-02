"""User token validation — the agent's inbound security boundary.

Validates the browser's PingOne login token (stored in ADK session state by
the bridge) before it is used as an RFC 8693 subject_token: signature via
JWKS, issuer, audience (the Financial Agent's own PingOne resource), `sub`
presence, and scope.

Agent Bridge already validates this token before storing it in session
state; this is an independent re-check before the token is spent, matching
the defense-in-depth pattern every other hop in this repo uses (the
extension validates, then the target it forwards to validates again).
"""

import os
from typing import Any

import httpx
from jose import JWTError, jwk, jwt

from config import IDP_ISSUER

EXPECTED_AUDIENCE = os.environ.get("IDP_REQUIRED_AUDIENCE", "finance-agent")
EXPECTED_SCOPE = os.environ.get("IDP_REQUIRED_SCOPE", "stripe_mcp:invoke")
JWKS_URL = f"{IDP_ISSUER}/jwks"

_jwks: dict[str, Any] | None = None


def _jwks_keys() -> dict[str, Any]:
    global _jwks
    if _jwks is None:
        response = httpx.get(JWKS_URL, timeout=10)
        response.raise_for_status()
        _jwks = response.json()
    return _jwks


def validate_user_token(token: str) -> dict[str, Any]:
    """Validate the browser's PingOne login token before using it as a subject token."""
    if not token:
        raise ValueError("user token is required")
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
        raise ValueError("invalid user token") from exc
    if not claims.get("sub"):
        raise ValueError("user token is missing sub")
    scopes = set(str(claims.get("scope", "")).split())
    if EXPECTED_SCOPE not in scopes:
        raise ValueError("user token scope mismatch")
    return claims
