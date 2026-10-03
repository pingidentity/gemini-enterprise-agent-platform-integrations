"""User token validation — the agent's inbound security boundary.

Validates the browser's PingOne login token (stored in ADK session state by
the bridge) before it is used as an RFC 8693 subject_token: signature via
JWKS, issuer, audience (Support Agent's own PingOne resource), `sub`
presence, and scope.

Agent Bridge already validates this token before storing it in session
state; this is an independent re-check before the token is spent, matching
the defense-in-depth pattern every other hop in this journey uses (the
extension validates, then the target it forwards to validates again).
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
