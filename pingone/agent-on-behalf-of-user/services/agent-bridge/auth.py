"""User token validation — the bridge's security boundary.

Every /chat request must present a PingOne access token minted by the Chat
UI's PKCE login. This module is the only thing that decides whether a request
carries a real user identity; everything downstream (session reuse, agent
invocation) trusts the `sub` this module returns.

The only deliberate gap: audience is not verified (`verify_aud=False`). The
user token is audienced to the Financial Agent resource; pinning it here is a
possible future hardening.
"""

import time

import httpx
from fastapi import HTTPException
from jose import JWTError, jwk, jwt

from config import IDP_ISSUER, JWKS_URI

# ── JWKS cache ───────────────────────────────────────────────────────────────

_jwks_cache: dict = {}
_jwks_fetched_at: float = 0.0
_JWKS_TTL = 3600


def _get_jwks() -> dict:
    global _jwks_cache, _jwks_fetched_at
    now = time.time()
    if _jwks_cache and now - _jwks_fetched_at < _JWKS_TTL:
        return _jwks_cache
    resp = httpx.get(JWKS_URI, timeout=10)
    resp.raise_for_status()
    _jwks_cache = resp.json()  # type: ignore[assignment]
    _jwks_fetched_at = now
    return _jwks_cache


# ── Token validation ─────────────────────────────────────────────────────────

def validate_user_token(token: str) -> dict:
    """Validate the user's PingOne token via JWKS and return the claims."""
    jwks_data = _get_jwks()
    try:
        unverified_header = jwt.get_unverified_header(token)
        kid = unverified_header.get("kid")
        key = next(
            (k for k in jwks_data["keys"] if k.get("kid") == kid),
            jwks_data["keys"][0] if jwks_data["keys"] else None,
        )
        if not key:
            raise HTTPException(status_code=401, detail="No matching key in JWKS")
        # PingOne JWKS omits the "alg" field; python-jose needs it explicit.
        alg = key.get("alg") or ("ES256" if key.get("kty") == "EC" else "RS256")
        public_key = jwk.construct(key, algorithm=alg)
        claims = jwt.decode(
            token,
            public_key,
            algorithms=["RS256", "ES256"],
            issuer=IDP_ISSUER,
            options={"verify_aud": False},
        )
    except JWTError as e:
        raise HTTPException(status_code=401, detail=f"Invalid token: {e}")

    if not claims.get("sub"):
        raise HTTPException(status_code=401, detail="Token missing sub claim")

    return claims
