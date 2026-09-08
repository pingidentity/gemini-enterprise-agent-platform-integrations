"""Deploy the OBO ADK agent to Agent Runtime, bound to the Agent Gateway."""

import os
import subprocess
import time

from google.cloud import storage
from google.genai.errors import ClientError

import agentplatform
from agentplatform import agent_engines, types
from dotenv import load_dotenv

# Load .env before importing agent — agent.py reads TOOL_MCP_URL at import time.
load_dotenv()

from agent import root_agent  # noqa: E402

PROJECT_ID = os.environ["GC_PROJECT_ID"]
REGION = os.environ["GC_REGION"]
TOOL_MCP_URL = os.environ["TOOL_MCP_URL"]
AGENT_GATEWAY = os.environ["GC_AGENT_GATEWAY"]
AGENT_DISPLAY_NAME = os.environ["AGENT_DISPLAY_NAME"]
AGENT_IDP_TOKEN_ENDPOINT = os.environ["AGENT_IDP_TOKEN_ENDPOINT"]
AGENT_IDP_CLIENT_ID = os.environ["AGENT_IDP_CLIENT_ID"]
AGENT_IDP_CLIENT_SECRET = os.environ["AGENT_IDP_CLIENT_SECRET"]
AGENT_IDP_SCOPE = os.environ.get("AGENT_IDP_SCOPE", "")


def staging_bucket() -> str:
    """Return the deploy staging bucket, creating gs://<project>-agent-staging if unset."""
    uri = os.environ.get("GC_STAGING_BUCKET")
    if uri:
        return uri
    name = f"{PROJECT_ID}-agent-staging"
    client = storage.Client(project=PROJECT_ID)
    if client.lookup_bucket(name) is None:
        print(f"Creating staging bucket gs://{name} ...")
        client.create_bucket(name, location=REGION)
    return f"gs://{name}"


def _org_id() -> str:
    """Return the numeric organization ID for PROJECT_ID."""
    out = subprocess.check_output(
        ["gcloud", "projects", "get-ancestors", PROJECT_ID, "--format=value(id,type)"],
        text=True,
    )
    for line in out.splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[1] == "organization":
            return parts[0]
    raise RuntimeError(f"No organization found for project {PROJECT_ID}")


def grant_egress(resource_name: str) -> None:
    """Grant iap.egressor to the engine so it can reach registered endpoints."""
    # resource_name format: projects/<project_number>/locations/<region>/reasoningEngines/<engine_id>
    parts = resource_name.split("/")
    project_number, engine_id = parts[1], parts[-1]
    principal = (
        f"principal://agents.global.org-{_org_id()}.system.id.goog"
        f"/resources/aiplatform/projects/{project_number}"
        f"/locations/{REGION}/reasoningEngines/{engine_id}"
    )
    print(f"Granting iap.egressor to engine {engine_id} ...")
    subprocess.check_call([
        "gcloud", "alpha", "iap", "web", "add-iam-policy-binding",
        "--resource-type=agent-registry",
        f"--region={REGION}",
        f"--member={principal}",
        "--role=roles/iap.egressor",
        f"--project={PROJECT_ID}",
    ])
    print("Egress grant done (propagation takes ~3 minutes).")


client = agentplatform.Client(project=PROJECT_ID, location=REGION)
app = agent_engines.AdkApp(agent=root_agent)

# Retry creation — after a teardown the gateway binding takes time to release.
_config = {
    "requirements": [
        # Pinned exactly — unbounded specs silently drift to versions that
        # break the engine (see journey CLAUDE.md gotchas).
        "google-cloud-aiplatform[agent_engines,adk]==1.165.1",
        "google-adk[a2a]==2.7.1",
        "mcp>=1.24,<2",
        "python-dotenv",
        "cloudpickle",
        "pydantic",
        "httpx",
    ],
    "extra_packages": ["pingone.py"],
    "staging_bucket": staging_bucket(),
    "display_name": AGENT_DISPLAY_NAME,
    "identity_type": types.IdentityType.AGENT_IDENTITY,
    "agent_gateway_config": {
        "agent_to_anywhere_config": {
            "agent_gateway": AGENT_GATEWAY,
        }
    },
    "env_vars": {
        "TOOL_MCP_URL": TOOL_MCP_URL,
        "GOOGLE_API_PREVENT_AGENT_TOKEN_SHARING_FOR_GCP_SERVICES": "false",
        "AGENT_IDP_TOKEN_ENDPOINT": AGENT_IDP_TOKEN_ENDPOINT,
        "AGENT_IDP_CLIENT_ID": AGENT_IDP_CLIENT_ID,
        "AGENT_IDP_CLIENT_SECRET": AGENT_IDP_CLIENT_SECRET,
        "AGENT_IDP_SCOPE": AGENT_IDP_SCOPE,
    },
}

print("Creating agent engine...")
for attempt in range(1, 11):
    try:
        remote_agent = client.agent_engines.create(agent=app, config=_config)
        break
    except ClientError as e:
        if "Another Agent Gateway is already active" in str(e) and attempt < 10:
            print(f"Gateway binding still releasing, waiting 30s (attempt {attempt}/10)...")
            time.sleep(30)
        else:
            raise

resource_name = remote_agent.api_resource.name
print("Deployed agent:", resource_name)
grant_egress(resource_name)
