"""
Deploy the AOBOU ADK agent to Agent Runtime, bound to the Agent Gateway.
"""

import json
import os
import subprocess
import time
import httpx
import agentplatform
from google.genai.errors import ClientError
from agentplatform import types
from agentplatform.frameworks import AdkApp
from google.cloud import storage

if not os.environ.get("GC_PROJECT_ID"):
    raw = subprocess.run(
        ["gcloud", "config", "get-value", "project"], capture_output=True, text=True
    ).stdout.strip()
    if not raw:
        raise SystemExit("no active gcloud project — run gcloud config set project <id>")
    os.environ["GC_PROJECT_ID"] = raw

from agent import root_agent
from config import (
    AGENT_CLIENT_ID,
    AGENT_CLIENT_SECRET,
    AGENT_DISPLAY_NAME,
    GC_AGENT_GATEWAY,
    GC_PROJECT_ID,
    GC_REGION,
    IDP_ISSUER,
    TOKEN_ENDPOINT,
    TOOL_MCP_URL,
    TOOL_SCOPE,
)
import gcp_helpers
from gcp_helpers import (
    delete_engine,
    die,
    engine_url,
    find_gateway_binding,
    gcloud,
    header,
    list_engines,
    ok,
    org_id,
)

# The gateway is configured by bare name; the full resource path is built here
# so the project-ID-string form is guaranteed by construction.
GATEWAY_RESOURCE = (
    f"projects/{GC_PROJECT_ID}/locations/{GC_REGION}/agentGateways/{GC_AGENT_GATEWAY}"
)

_DEPLOY_T0 = time.monotonic()


def preflight() -> None:
    header("preflight")
    ok(f"project={GC_PROJECT_ID} region={GC_REGION}")
    ok(f"tool={TOOL_MCP_URL}")

    # The gateway must exist before an engine can bind to it — fail here in
    # seconds rather than 30s into the engine create.
    gw_name = GC_AGENT_GATEWAY
    describe = subprocess.run(
        ["gcloud", "beta", "network-services", "agent-gateways", "describe",
         gw_name, "--project", GC_PROJECT_ID, "--location", GC_REGION,
         "--format=value(name)"],
        capture_output=True, text=True,
    )
    if describe.returncode != 0 or not describe.stdout.strip():
        die(f"gateway not found: {gw_name} (project {GC_PROJECT_ID}, location {GC_REGION})",
            "Gateways are created in the console (pick the 'Allow Policy (legacy)'\n"
            "  policy model), then set GC_AGENT_GATEWAY in .env to its bare name\n"
            "  (e.g. aobou-agent-gateway).")
    ok(f"gateway={gw_name} (exists in {GC_PROJECT_ID})")

    # Mint the agent's identity token for real — every hop of the demo
    # depends on this, and it catches bad credentials in seconds instead of
    # after a multi-minute engine deploy.
    try:
        resp = httpx.post(
            TOKEN_ENDPOINT,
            data={"grant_type": "client_credentials", "scope": TOOL_SCOPE},
            auth=(AGENT_CLIENT_ID, AGENT_CLIENT_SECRET),
            headers={"Content-Type": "application/x-www-form-urlencoded"},
            timeout=15,
        )
    except httpx.HTTPError as e:
        die(f"PingOne unreachable at {TOKEN_ENDPOINT}", str(e))
    if resp.status_code == 400:
        die("PingOne token mint rejected (HTTP 400)",
            "Check AGENT_CLIENT_ID / AGENT_CLIENT_SECRET in .env against the\n"
            "  PingOne application, and that the app has the Token Exchange /\n"
            "  Client Credentials grants with the resource's scope assigned.")
    if resp.status_code == 403:
        die("PingOne token mint forbidden (HTTP 403)",
            "The application's scope grant is missing — assign the resource\n"
            "  scope in PingOne (Applications → <agent app> → Grants).")
    if resp.status_code != 200:
        die(f"PingOne token mint failed (HTTP {resp.status_code})", resp.text[:300])
    if not resp.json().get("access_token"):
        die("PingOne returned no access_token", resp.text[:300])
    ok(f"token mint — client {AGENT_CLIENT_ID[:8]}… scope={TOOL_SCOPE}")


def staging_bucket() -> str:
    """Engine deploys stage code + deps into gs://<project>-agent-staging;
    create it on first use."""
    name = f"{GC_PROJECT_ID}-agent-staging"
    client = storage.Client(project=GC_PROJECT_ID)
    if client.lookup_bucket(name) is None:
        print(f"  · creating staging bucket gs://{name} …")
        client.create_bucket(name, location=GC_REGION)
    return f"gs://{name}"


def cleanup_stale_engines() -> None:
    """Delete engines sharing our display name. A redeploy replaces them by
    definition; without this, every failed post-check orphans an engine and
    the next deploy trips over it."""
    stale = [e for e in list_engines() if e.get("displayName") == AGENT_DISPLAY_NAME]
    for e in stale:
        engine_id = e["name"].rsplit("/", 1)[-1]
        print(f"  · deleting stale engine {engine_id} …")
        delete_engine(engine_id)
    if stale:
        # List is eventually consistent — poll until the deletions land so
        # the create below starts from a clean slate.
        for _ in range(20):
            if not [e for e in list_engines() if e.get("displayName") == AGENT_DISPLAY_NAME]:
                return
            time.sleep(3)
        die("stale engines did not delete in time — check console")


def create_engine() -> str:
    header("deploy")
    print(f"  · creating agent engine {AGENT_DISPLAY_NAME!r} in {GC_REGION} …")
    print(f"  · this takes 3–5 minutes (build, deploy, health-check) …")
    client = agentplatform.Client(project=GC_PROJECT_ID, location=GC_REGION)
    app = AdkApp(agent=root_agent)

    env_vars = {
        # Everything config.py requires at engine-startup import time (it
        # runs agent.py → config.py, so all require_env values must be here).
        "GC_PROJECT_ID": GC_PROJECT_ID,
        "GC_REGION": GC_REGION,
        "GC_AGENT_GATEWAY": GC_AGENT_GATEWAY,
        "AGENT_DISPLAY_NAME": AGENT_DISPLAY_NAME,
        "TOOL_MCP_URL": TOOL_MCP_URL,
        "IDP_ISSUER": IDP_ISSUER,
        "AGENT_CLIENT_ID": AGENT_CLIENT_ID,
        "AGENT_CLIENT_SECRET": AGENT_CLIENT_SECRET,
        "TOOL_SCOPE": TOOL_SCOPE,
        # Let the agent's own PingOne token ride on MCP egress through the gateway.
        "GOOGLE_API_PREVENT_AGENT_TOKEN_SHARING_FOR_GCP_SERVICES": "false",
    }
    config = {
        "requirements": "requirements.txt",
        "extra_packages": ["config.py", "auth.py", "pingone.py"],
        "staging_bucket": staging_bucket(),
        "display_name": AGENT_DISPLAY_NAME,
        "identity_type": types.IdentityType.AGENT_IDENTITY,
        "agent_gateway_config": {"agent_to_anywhere_config": {"agent_gateway": GATEWAY_RESOURCE}},
        "env_vars": env_vars,
    }

    remote_agent = None
    for attempt in range(1, 11):
        try:
            remote_agent = client.runtimes.create(agent=app, config=config)
            break
        except ClientError as e:
            msg = str(e)
            # Recreating right after a teardown: the old gateway binding
            # takes a few minutes to release (one-gateway-per-project rule).
            if "Another Agent Gateway is already active" in msg and attempt < 10:
                print(f"  · another gateway holds the project slot, waiting 30s (attempt {attempt}/10) …")
                time.sleep(30)
            elif "was not found" in msg and "agentGateway" in msg.replace("Gateways", "Gateway"):
                die(f"gateway not found: {GC_AGENT_GATEWAY}",
                    "The engine deploy validates the gateway reference and this\n"
                    "  gateway does not exist at that project/location. Gateways are\n"
                    "  created in the console (pick the 'Allow Policy (legacy)' policy\n"
                    "  model), then set GC_AGENT_GATEWAY in .env to its bare name\n"
                    "  (e.g. aobou-agent-gateway).")
            else:
                die(f"engine create failed: {msg}")
    if remote_agent is None:
        die("agent engine was never created — see errors above")

    resource_name = remote_agent.api_resource.name
    ok(f"engine created: {resource_name.rsplit('/', 1)[-1]}")
    return resource_name


def grant_egress(resource_name: str) -> None:
    """Grant roles/iap.egressor to the engine so its egress passes the gateway."""
    # resource_name: projects/<project_number>/locations/<region>/reasoningEngines/<engine_id>
    parts = resource_name.split("/")
    principal = (
        f"principal://agents.global.org-{org_id()}.system.id.goog"
        f"/resources/aiplatform/projects/{parts[1]}"
        f"/locations/{GC_REGION}/reasoningEngines/{parts[-1]}"
    )
    print("  · granting roles/iap.egressor to engine principal …")
    subprocess.check_call([
        "gcloud", "alpha", "iap", "web", "add-iam-policy-binding",
        "--resource-type=agent-registry",
        f"--region={GC_REGION}",
        f"--member={principal}",
        "--role=roles/iap.egressor",
        f"--project={GC_PROJECT_ID}",
    ], stdout=subprocess.DEVNULL)
    ok("egress grant done (gateway-side propagation takes ~3 minutes)")


def postdeploy(resource_name: str) -> None:
    header("postdeploy")
    engine_id = resource_name.rsplit("/", 1)[-1]

    # 1. The engine this deploy created is fetchable by ID (not a name-count
    #    check — the list is eventually consistent, ghosts must not fail us).
    got = subprocess.run(
        ["curl", "-s", "-f", "-H", f"Authorization: Bearer {gcloud('auth', 'print-access-token')}",
         engine_url(engine_id)],
        capture_output=True, text=True,
    )
    if got.returncode != 0:
        die(f"created engine {engine_id} is not fetchable — create did not land")
    engine = json.loads(got.stdout)
    if engine.get("displayName") != AGENT_DISPLAY_NAME:
        die(f"engine {engine_id} has display name {engine.get('displayName')!r}, expected {AGENT_DISPLAY_NAME!r}")
    ok(f"engine {engine_id} is live")

    # 2. The engine carries the expected gateway binding.
    gateway = find_gateway_binding(engine)
    if gateway is None:
        die("engine spec shows no gateway binding",
            "The engine was created without agentGatewayConfig — check the\n"
            "  agent_gateway_config block in deploy.py.")
    if gateway != GATEWAY_RESOURCE:
        die(f"engine bound to {gateway}, expected {GATEWAY_RESOURCE}")
    ok(f"gateway binding: {GC_AGENT_GATEWAY}")

    # 3. The engine's agent principal holds roles/iap.egressor.
    project_number = gcloud("projects", "describe", GC_PROJECT_ID,
                             "--format=value(projectNumber)")
    principal = (
        f"principal://agents.global.org-{org_id()}.system.id.goog"
        f"/resources/aiplatform/projects/{project_number}"
        f"/locations/{GC_REGION}/reasoningEngines/{engine_id}"
    )
    policy = json.loads(gcloud(
        "alpha", "iap", "web", "get-iam-policy",
        "--resource-type=agent-registry", f"--region={GC_REGION}",
        f"--project={GC_PROJECT_ID}", "--format=json",
    ))
    bound = any(
        b.get("role") == "roles/iap.egressor" and principal in b.get("members", [])
        for b in policy.get("bindings", [])
    )
    if not bound:
        die(f"{principal} is missing roles/iap.egressor",
            "The grant did not land (or landed under a stale principal). Re-run\n"
            "make deploy, or add the binding manually with gcloud alpha iap web\n"
            "add-iam-policy-binding --resource-type=agent-registry.")
    ok("iap.egressor granted to engine principal")

    elapsed = time.monotonic() - _DEPLOY_T0
    print(f"""
{gcp_helpers.LINE}
  ✔ DEPLOY COMPLETE — {AGENT_DISPLAY_NAME}  ({elapsed:.0f}s)
{gcp_helpers.LINE}
    engine:   {resource_name}
    gateway:  {GC_AGENT_GATEWAY}  (egress live after ~3 min propagation)
{gcp_helpers.LINE}""")


if __name__ == "__main__":
    try:
        preflight()
        cleanup_stale_engines()
        resource_name = create_engine()
        grant_egress(resource_name)
        postdeploy(resource_name)
    except KeyboardInterrupt:
        die("interrupted")
