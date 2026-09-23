import os


def require_env(name: str) -> str:
    """Return an env var or fail with a named, actionable error."""
    value = os.environ.get(name)
    if not value:
        raise RuntimeError(f"missing required env var: {name} (copy .env.sample to .env and fill in)")
    return value


GC_PROJECT_ID = require_env("GC_PROJECT_ID")
GC_REGION = require_env("GC_REGION")
GC_AGENT_GATEWAY = require_env("GC_AGENT_GATEWAY")
AGENT_DISPLAY_NAME = require_env("AGENT_DISPLAY_NAME")
AGENT_CLIENT_ID = require_env("AGENT_CLIENT_ID")
AGENT_CLIENT_SECRET = require_env("AGENT_CLIENT_SECRET")
TOOL_MCP_URL = require_env("TOOL_MCP_URL")
TOOL_SCOPE = require_env("TOOL_SCOPE")
IDP_ISSUER = require_env("IDP_ISSUER").rstrip("/")
TOKEN_ENDPOINT = f"{IDP_ISSUER}/token"
