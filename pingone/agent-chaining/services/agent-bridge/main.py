"""Agent bridge — validates the user's PingOne token and invokes Agent Runtime.

POST /chat:
  Authorization: Bearer <user_pingone_token>
  {"message": "..."}

  1. Validate the user token via PingOne JWKS.
  2. Create (or reuse) an ADK session with user_token in state.
  3. Call agent.stream_query; return the text response.
"""

from datetime import datetime, timezone
import os
import time
from uuid import uuid4

import httpx
from dotenv import load_dotenv
from fastapi import FastAPI, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from jose import JWTError, jwk, jwt
from pydantic import BaseModel

load_dotenv()

import agentplatform
from agentplatform import types as agent_types

# ── Config ────────────────────────────────────────────────────────────────────

GC_PROJECT_ID     = os.environ["GC_PROJECT_ID"]
GC_REGION         = os.environ["GC_REGION"]
AGENT_ENGINE_NAME = os.environ["AGENT_ENGINE_NAME"]
CORS_ORIGIN       = os.environ["CORS_ORIGIN"]
PINGONE_ISSUER    = os.environ["PINGONE_ISSUER"].rstrip("/")
# The browser's PKCE login token is audienced to Support Agent's own PingOne
# resource (see agent-chaining/CLAUDE.md's PingOne setup section) — enforced
# here at the front door, and independently re-checked inside Support Agent
# itself before it's ever used as a token-exchange subject_token.
EXPECTED_AUDIENCE = os.environ.get("EXPECTED_AUDIENCE", "support-agent")

JWKS_URI = f"{PINGONE_ISSUER}/jwks"

# ── JWKS cache ────────────────────────────────────────────────────────────────

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


# ── Token validation ──────────────────────────────────────────────────────────

def _validate_user_token(token: str) -> dict:
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
            issuer=PINGONE_ISSUER,
            audience=EXPECTED_AUDIENCE,
        )
    except JWTError as e:
        raise HTTPException(status_code=401, detail=f"Invalid token: {e}")

    if not claims.get("sub"):
        raise HTTPException(status_code=401, detail="Token missing sub claim")

    return claims


# ── Agent Runtime ─────────────────────────────────────────────────────────────

_client = agentplatform.Client(project=GC_PROJECT_ID, location=GC_REGION)
_agent  = _client.agent_engines.get(name=AGENT_ENGINE_NAME)

# user_sub → (session_id, last_token)
_sessions: dict[str, tuple[str, str]] = {}


def _create_session(user_sub: str, user_token: str) -> str:
    print(f"[bridge] creating session user={user_sub} engine={AGENT_ENGINE_NAME}", flush=True)
    op = _client.agent_engines.sessions.create(
        name=AGENT_ENGINE_NAME,
        user_id=user_sub,
        config=agent_types.CreateAgentEngineSessionConfig(
            session_state={"user_token": user_token},
        ),
    )
    session_name = op.response.name
    session_id = session_name.split("/")[-1]
    _client.agent_engines.sessions.events.append(
        name=session_name,
        author="system",
        invocation_id=str(uuid4()),
        timestamp=datetime.now(timezone.utc),
        config=agent_types.AppendAgentEngineSessionEventConfig(
            actions=agent_types.EventActions(state_delta={"user_token": user_token}),
        ),
    )
    print(f"[bridge] created session_id={session_id}", flush=True)
    _sessions[user_sub] = (session_id, user_token)
    return session_id


def _ensure_session(user_sub: str, user_token: str) -> str:
    """Create or reuse an ADK session. On token change, create a new session."""
    if user_sub in _sessions:
        session_id, last_token = _sessions[user_sub]
        if last_token == user_token:
            print(f"[bridge] reusing session_id={session_id}", flush=True)
            return session_id
    return _create_session(user_sub, user_token)


def _run_agent(user_sub: str, session_id: str, message: str) -> str:
    print(f"[bridge] stream_query user={user_sub} session_id={session_id} message={message!r}", flush=True)
    text_parts: list[str] = []
    for event in _agent.stream_query(
        message=message,
        user_id=user_sub,
        session_id=session_id,
    ):
        print(f"[bridge] event author={event.get('author')} keys={list(event.keys())}", flush=True)
        content = event.get("content", {})
        for part in content.get("parts", []):
            if "text" in part:
                text_parts.append(part["text"])
        actions = event.get("actions", {})
        function_call = content.get("parts", [{}])[0].get("function_call") if content.get("parts") else None
        function_response = content.get("parts", [{}])[0].get("function_response") if content.get("parts") else None
        if function_call or function_response or actions.get("state_delta"):
            print(f"[bridge] non_text_event function_call={bool(function_call)} function_response={bool(function_response)}", flush=True)
    reply = "\n".join(text_parts).strip()
    if not reply:
        raise RuntimeError("Agent Runtime returned no text response; inspect the Support Agent session events")
    return reply


# ── FastAPI app ────────────────────────────────────────────────────────────────

app = FastAPI()
app.add_middleware(
    CORSMiddleware,
    allow_origins=[CORS_ORIGIN],
    allow_methods=["POST", "GET"],
    allow_headers=["Authorization", "Content-Type"],
)


class ChatRequest(BaseModel):
    message: str


@app.get("/health")
def health():
    return {"status": "ok"}


@app.post("/chat")
async def chat(request: Request, body: ChatRequest):
    auth = request.headers.get("Authorization", "")
    if not auth.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="Missing Bearer token")
    user_token = auth[7:]

    claims = _validate_user_token(user_token)
    user_sub = claims["sub"]

    print(f"[bridge] authenticated user sub={user_sub}", flush=True)

    session_id = _ensure_session(user_sub, user_token)

    try:
        reply = _run_agent(user_sub, session_id, body.message)
        return {"response": reply}
    except Exception as exc:
        raise HTTPException(status_code=500, detail=str(exc)) from exc
