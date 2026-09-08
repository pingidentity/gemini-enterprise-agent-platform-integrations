"""Deploy the Support Agent to Agent Runtime."""

import os
import subprocess

from dotenv import load_dotenv
import agentplatform
from agentplatform import agent_engines, types
from google.cloud import storage

load_dotenv()
from agent import root_agent  # noqa: E402

PROJECT_ID = os.environ["GC_PROJECT_ID"]
REGION = os.environ["GC_REGION"]
AGENT_GATEWAY = os.environ["GC_AGENT_GATEWAY"]
AGENT_DISPLAY_NAME = os.environ["AGENT_DISPLAY_NAME"]


def staging_bucket() -> str:
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
    project_number, engine_id = resource_name.split("/")[1], resource_name.split("/")[-1]
    principal = (
        f"principal://agents.global.org-{_org_id()}.system.id.goog"
        f"/resources/aiplatform/projects/{project_number}/locations/{REGION}/reasoningEngines/{engine_id}"
    )
    subprocess.check_call([
        "gcloud", "alpha", "iap", "web", "add-iam-policy-binding",
        "--resource-type=agent-registry", f"--region={REGION}",
        f"--member={principal}", "--role=roles/iap.egressor", f"--project={PROJECT_ID}",
    ])


client = agentplatform.Client(project=PROJECT_ID, location=REGION)
app = agent_engines.AdkApp(agent=root_agent)
config = {
    "requirements": [
        # Pinned exactly, not >=1.165.1 — an unbounded lower-bound spec let the
        # remote build resolve a newer release that renamed/removed
        # agentplatform.agent_engines, failing the deployed engine at startup
        # with "No module named 'agentplatform.agent_engines'" even though the
        # local venv (built at a different moment) resolved to 1.165.1 fine.
        "google-cloud-aiplatform[agent_engines,adk]==1.165.1",
        # Pinned exactly — an unbounded <3 spec let the remote build resolve
        # google-adk 2.8.0 (local venv had 2.7.1, where this exact agent was
        # verified working), and the deployed engine's workers then died
        # mid-stream on every successful tool call (error-path tool results
        # streamed fine; success-path runs returned zero events after the
        # function_call event). Same failure class as the aiplatform pin below.
        "google-adk[a2a]==2.7.1",
        "a2a-sdk==1.1.2",
        "httpx", "python-dotenv", "cloudpickle", "pydantic",
        "python-jose[cryptography]",
    ],
    "extra_packages": ["agent.py", "pingone.py"],
    "staging_bucket": staging_bucket(),
    "display_name": AGENT_DISPLAY_NAME,
    "identity_type": types.IdentityType.AGENT_IDENTITY,
    "agent_gateway_config": {"agent_to_anywhere_config": {"agent_gateway": AGENT_GATEWAY}},
    "env_vars": {key: value for key, value in os.environ.items() if key.startswith(("GC_", "A2A_", "SUPPORT_AGENT_", "AGENT_IDP_", "LOCAL_"))},
}
remote_agent = client.agent_engines.create(agent=app, config=config)
print("Deployed Support Agent:", remote_agent.api_resource.name)
grant_egress(remote_agent.api_resource.name)
