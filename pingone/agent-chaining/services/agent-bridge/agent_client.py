"""
Agent Runtime integration — the session boundary and message stream.
  - Owns everything that talks to the deployed Reasoning Engine
"""

from datetime import datetime, timezone
from typing import NamedTuple
from uuid import uuid4

import agentplatform
from agentplatform._genai.types import (
    AppendRuntimeSessionEventConfig,
    CreateRuntimeSessionConfig,
    EventActions,
)
from config import AGENT_ENGINE_NAME, GC_PROJECT_ID, GC_REGION


_client = agentplatform.Client(project=GC_PROJECT_ID, location=GC_REGION)
_agent = _client.runtimes.get(name=AGENT_ENGINE_NAME)


class _SessionBinding(NamedTuple):
    session_id: str
    user_token: str
_sessions: dict[str, _SessionBinding] = {}


def _create_session(user_sub: str, user_token: str) -> str:
    print(f"[bridge] creating session user={user_sub} engine={AGENT_ENGINE_NAME}", flush=True)
    op = _client.sessions.create(
        name=AGENT_ENGINE_NAME,
        user_id=user_sub,
        config=CreateRuntimeSessionConfig(
            session_state={"user_token": user_token},
        ),
    )
    session_name = op.response.name
    session_id = session_name.split("/")[-1]
    # Re-seed user_token as a session event state-delta: Support Agent reads it
    # from ToolContext.state, and the appended event guarantees the state is
    # visible to the engine's session service even if the create-call
    # session_state were dropped.
    _client.sessions.events.append(
        name=session_name,
        author="system",
        invocation_id=str(uuid4()),
        timestamp=datetime.now(timezone.utc),
        config=AppendRuntimeSessionEventConfig(
            actions=EventActions(state_delta={"user_token": user_token}),
        ),
    )
    print(f"[bridge] created session_id={session_id}", flush=True)
    _sessions[user_sub] = _SessionBinding(session_id, user_token)
    return session_id


def ensure_session(user_sub: str, user_token: str) -> str:
    """Create or reuse an ADK session. On token change, create a new session."""
    binding = _sessions.get(user_sub)
    if binding and binding.user_token == user_token:
        print(f"[bridge] reusing session_id={binding.session_id}", flush=True)
        return binding.session_id
    return _create_session(user_sub, user_token)


def run_agent(user_sub: str, session_id: str, message: str) -> str:
    print(f"[bridge] stream_query user={user_sub} session_id={session_id} message={message!r}", flush=True)
    text_parts: list[str] = []
    for event in _agent.stream_query(message=message, user_id=user_sub, session_id=session_id):
        content = event.get("content") or {}
        for part in content.get("parts") or []:
            if "text" in part:
                text_parts.append(part["text"])
    reply = "\n".join(text_parts).strip()
    if not reply:
        raise RuntimeError("Agent Runtime returned no text response; inspect the Support Agent session events")
    return reply
