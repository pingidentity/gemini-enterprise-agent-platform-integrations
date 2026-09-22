"""Agent bridge — the boundary between the browser and Agent Runtime.

POST /chat:
  Authorization: Bearer <user_pingone_token>
  {"message": "..."}

The request path, in order (each step lives in the module that owns it):
  1. auth.py    — validate the user token (the security boundary)
  2. this file  — create/reuse an ADK session carrying user_token in state
  3. this file  — stream the message through the agent and return its text
"""

import httpx  # noqa: F401  (re-exported for tests that stub it)
from dotenv import load_dotenv
from fastapi import FastAPI, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel

load_dotenv()

from agentplatform import types as agent_types
import agentplatform

from auth import validate_user_token
from config import AGENT_ENGINE_NAME, CORS_ORIGIN, GC_PROJECT_ID, GC_REGION

# ── Agent Runtime client ─────────────────────────────────────────────────────

_client = agentplatform.Client(project=GC_PROJECT_ID, location=GC_REGION)
_agent = _client.agent_engines.get(name=AGENT_ENGINE_NAME)

# ── Session cache ────────────────────────────────────────────────────────────
# user_sub → (session_id, last_token). Deliberately in-memory: one lookup at
# session creation, then the bridge reuses the cached pair. Agent Runtime's
# sessions.list is quota-limited and flaky in this project, so the stateless
# alternative (listing sessions per request) was tried and reverted 2026-09-07.
# Accepted limitation: per-instance cache — scale-out would orphan sessions.
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
    # Log only completion + session id; op.response contains session_state with
    # the raw user token, which must never be logged.
    print(f"[bridge] session op done={op.done} error={op.error}", flush=True)
    session_id = op.response.name.split("/")[-1]
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
        content = event.get("content", {})
        for part in content.get("parts", []):
            if "text" in part:
                text_parts.append(part["text"])
    return "\n".join(text_parts).strip()


# ── FastAPI app ──────────────────────────────────────────────────────────────

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

    claims = validate_user_token(user_token)
    user_sub = claims["sub"]

    print(f"[TOKEN:user] sub={user_sub}", flush=True)

    session_id = _ensure_session(user_sub, user_token)

    try:
        reply = _run_agent(user_sub, session_id, body.message)
        return {"response": reply}
    except Exception as exc:
        raise HTTPException(status_code=500, detail=str(exc)) from exc
