"""
Token validation — the security boundary of this service.

Every /chat request must present a PingOne access token:
  - Validation: signature (JWKS), issuer, audience, sub.
"""

import httpx
import time
from typing import Any
from fastapi import HTTPException
from jose import JWTError, jwk, jwt
from config import IDP_ISSUER, IDP_REQUIRED_AUDIENCE, JWKS_URI


_jwks_cache: dict[str, Any] = {}
_jwks_fetched_at: float = 0.0
_JWKS_TTL = 3600


def _get_jwks() -> dict[str, Any]:
    global _jwks_cache, _jwks_fetched_at
    now = time.time()
    if _jwks_cache and now - _jwks_fetched_at < _JWKS_TTL:
        return _jwks_cache
    resp = httpx.get(JWKS_URI, timeout=10)
    resp.raise_for_status()
    _jwks_cache = resp.json()
    _jwks_fetched_at = now
    return _jwks_cache


def validate_user_token(token: str) -> dict[str, Any]:
    jwks_data = _get_jwks()
    try:
        header = jwt.get_unverified_header(token)
        keys = jwks_data["keys"]
        key = next(
            (k for k in keys if k.get("kid") == header.get("kid")),
            keys[0] if keys else None,
        )
        if not key:
            raise HTTPException(status_code=401, detail="No matching key in JWKS")
        alg = key.get("alg") or ("ES256" if key.get("kty") == "EC" else "RS256")
        claims = jwt.decode(
            token,
            jwk.construct(key, algorithm=alg),
            algorithms=["RS256", "ES256"],
            issuer=IDP_ISSUER,
            audience=IDP_REQUIRED_AUDIENCE,
        )
    except JWTError as e:
        raise HTTPException(status_code=401, detail=f"Invalid token: {e}")

    if not claims.get("sub"):
        raise HTTPException(status_code=401, detail="Token missing sub claim")

    return claims
