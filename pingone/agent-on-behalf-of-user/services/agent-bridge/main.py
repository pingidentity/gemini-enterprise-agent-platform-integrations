"""
Agent bridge — the boundary between the browser and Agent Runtime.

POST /chat: Authorization: Bearer <user_pingone_token>, body {"message": "..."}.
auth.py validates the token (the security boundary); agent_client.py owns the
Agent Runtime session and stream; this file is the HTTP surface.
"""

from dotenv import load_dotenv
from fastapi import FastAPI, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
load_dotenv()
from agent_client import ensure_session, run_agent
from auth import validate_user_token
from config import CORS_ORIGIN

app = FastAPI()
app.add_middleware(
    CORSMiddleware,
    allow_origins=[CORS_ORIGIN],
    allow_methods=["POST", "GET"],
    allow_headers=["Authorization", "Content-Type"],
)


@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok"}


@app.post("/chat")
async def chat(request: Request) -> dict[str, str]:
    auth_header = request.headers.get("Authorization", "")
    if not auth_header.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="Missing Bearer token")
    user_token = auth_header[len("Bearer "):]

    claims = validate_user_token(user_token)
    sub = claims["sub"]
    if not isinstance(sub, str):
        raise HTTPException(status_code=401, detail="Token sub claim is not a string")
    user_sub = sub
    print(f"[TOKEN:user] sub={user_sub}", flush=True)

    session_id = ensure_session(user_sub, user_token)

    try:
        message = (await request.json())["message"]
        return {"response": run_agent(user_sub, session_id, message)}
    except Exception as exc:
        raise HTTPException(status_code=500, detail=str(exc)) from exc
