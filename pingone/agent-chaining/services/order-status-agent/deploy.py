"""Deploy the Order Status Agent to Agent Runtime, bound to the Agent Gateway."""

import os
import subprocess
import time

from google.cloud import storage
from google.genai.errors import ClientError

import agentplatform
from agentplatform import agent_engines, types
from dotenv import load_dotenv

load_dotenv()
from agent import root_agent  # noqa: E402

PROJECT_ID = os.environ["GC_PROJECT_ID"]
REGION = os.environ["GC_REGION"]


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
app = root_agent
config = {
    # Pinned exactly, not >=1.165.1 — see support-agent/deploy.py's comment:
    # an unbounded lower bound let a support-agent redeploy resolve a newer
    # release that renamed/removed agentplatform.agent_engines, failing the
    # deployed engine at startup. Applying the same pin here preemptively.
    # google-adk/a2a-sdk pinned exactly: an unbounded <3/<2 spec let the
    # support-agent's remote build silently resolve google-adk 2.8.0 (local
    # venv had 2.7.1, the verified-working version), and its deployed workers
    # then died mid-stream on every successful tool call. Same failure class
    # as the aiplatform pin above.
    "requirements": ["google-cloud-aiplatform[agent_engines,adk]==1.165.1", "google-adk[a2a]==2.7.1", "a2a-sdk==1.1.2", "httpx", "python-dotenv", "cloudpickle", "pydantic", "python-jose[cryptography]", "sse-starlette>=2.1.0"],
    "extra_packages": ["agent.py", "pingone.py", "protocol.py"],
    "staging_bucket": staging_bucket(),
    "display_name": os.environ["AGENT_DISPLAY_NAME"],
    "identity_type": types.IdentityType.AGENT_IDENTITY,
    "agent_gateway_config": {"agent_to_anywhere_config": {"agent_gateway": os.environ["GC_AGENT_GATEWAY"]}},
    "env_vars": {key: value for key, value in os.environ.items() if key.startswith(("GC_", "A2A_", "MCP_", "AGENT_", "LOCAL_"))},
}

print("Creating agent engine...")
for attempt in range(1, 11):
    try:
        remote_agent = client.agent_engines.create(agent=app, config=config)
        break
    except ClientError as e:
        if "Another Agent Gateway is already active" in str(e) and attempt < 10:
            print(f"Gateway binding still releasing, waiting 30s (attempt {attempt}/10)...")
            time.sleep(30)
        else:
            raise

resource_name = remote_agent.api_resource.name
print("Deployed Order Status Agent:", resource_name)
grant_egress(resource_name)
