"""Deploy the BAATT ADK agent to Agent Runtime, bound to the Agent Gateway.

Pipeline: preflight (config + live PingOne token mint) → engine create →
egress grant → postdeploy (gateway binding + iap.egressor verified from IAM).
Failures print a one-screen diagnosis and exit non-zero — no raw tracebacks
for expected failure modes.
"""

import json
import subprocess
import sys
import time
import warnings

# The ADK PLUGGABLE_AUTH experimental flag fires a UserWarning on import;
# it's expected and would otherwise pollute the deploy output.
warnings.filterwarnings("ignore", message=r".*\[EXPERIMENTAL\].*")

import httpx
from google.genai.errors import ClientError

import agentplatform
from agentplatform import agent_engines, types
from google.cloud import storage

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

LINE = "─" * 64


def header(title: str) -> None:
    print(f"\n{LINE}\n  {title}\n{LINE}")


def ok(msg: str) -> None:
    print(f"  ✔ {msg}")


def die(msg: str, hint: str | None = None) -> None:
    print(f"\n  ✗ DEPLOY FAILED — {msg}")
    if hint:
        print(f"\n{hint}")
    sys.exit(1)


def _gcloud(*args: str) -> str:
    return subprocess.run(
        ["gcloud", *args], capture_output=True, text=True, check=True
    ).stdout.strip()


def preflight() -> None:
    header("preflight")
    ok(f"project={GC_PROJECT_ID} region={GC_REGION}")
    ok(f"tool={TOOL_MCP_URL}")

    # The gateway must exist before an engine can bind to it — fail here in
    # seconds rather than 30s into the engine create.
    gw_name = GC_AGENT_GATEWAY.rsplit("/", 1)[-1]
    gw_project = GC_AGENT_GATEWAY.split("/")[1]
    describe = subprocess.run(
        ["gcloud", "beta", "network-services", "agent-gateways", "describe",
         gw_name, "--project", gw_project, "--location", GC_REGION,
         "--format=value(name)"],
        capture_output=True, text=True,
    )
    if describe.returncode != 0 or not describe.stdout.strip():
        die(f"gateway not found: {gw_name} (project {gw_project}, location {GC_REGION})",
            "Gateways are created in the console (pick the 'Allow Policy (legacy)'\n"
            "  policy model), then set GC_AGENT_GATEWAY in .env to its full resource\n"
            "  name: projects/<project>/locations/us-central1/agentGateways/<name>")
    ok(f"gateway={gw_name} (exists in {gw_project})")

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


def _org_id() -> str:
    out = _gcloud("projects", "get-ancestors", GC_PROJECT_ID,
                  "--format=value(id,type)")
    for line in out.splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[1] == "organization":
            return parts[0]
    die(f"no organization found for project {GC_PROJECT_ID}")


def _engine_url(engine_id: str) -> str:
    return (f"https://aiplatform.googleapis.com/v1/projects/{GC_PROJECT_ID}"
            f"/locations/{GC_REGION}/reasoningEngines/{engine_id}")


def _delete_engine(engine_id: str) -> None:
    subprocess.run(
        ["curl", "-s", "-o", "/dev/null", "-X", "DELETE",
         "-H", f"Authorization: Bearer {_gcloud('auth', 'print-access-token')}",
         f"{_engine_url(engine_id)}?force=true"],
        capture_output=True, text=True, check=True,
    )


def cleanup_stale_engines() -> None:
    """Delete engines sharing our display name. A redeploy replaces them by
    definition; without this, every failed post-check orphans an engine and
    the next deploy trips over it."""
    stale = [e for e in _list_engines() if e.get("displayName") == AGENT_DISPLAY_NAME]
    for e in stale:
        engine_id = e["name"].rsplit("/", 1)[-1]
        print(f"  · deleting stale engine {engine_id} …")
        _delete_engine(engine_id)
    if stale:
        # List is eventually consistent — poll until the deletions land so
        # the create below starts from a clean slate.
        for _ in range(20):
            if not [e for e in _list_engines() if e.get("displayName") == AGENT_DISPLAY_NAME]:
                return
            time.sleep(3)
        die("stale engines did not delete in time — check console")


def create_engine() -> str:
    header("deploy")
    print(f"  · creating agent engine {AGENT_DISPLAY_NAME!r} in {GC_REGION} …")
    print(f"  · this takes 3–5 minutes (build, deploy, health-check) …")
    client = agentplatform.Client(project=GC_PROJECT_ID, location=GC_REGION)
    app = agent_engines.AdkApp(agent=root_agent)
    started = time.monotonic()

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
        "extra_packages": ["config.py", "pingone.py"],
        "staging_bucket": staging_bucket(),
        "display_name": AGENT_DISPLAY_NAME,
        "identity_type": types.IdentityType.AGENT_IDENTITY,
        "agent_gateway_config": {"agent_to_anywhere_config": {"agent_gateway": GC_AGENT_GATEWAY}},
        "env_vars": env_vars,
    }

    remote_agent = None
    for attempt in range(1, 11):
        try:
            remote_agent = client.agent_engines.create(agent=app, config=config)
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
                    "  model), then set GC_AGENT_GATEWAY in .env to its full resource\n"
                    "  name: projects/<project>/locations/us-central1/agentGateways/<name>")
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
        f"principal://agents.global.org-{_org_id()}.system.id.goog"
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
        ["curl", "-s", "-f", "-H", f"Authorization: Bearer {_gcloud('auth', 'print-access-token')}",
         _engine_url(engine_id)],
        capture_output=True, text=True,
    )
    if got.returncode != 0:
        die(f"created engine {engine_id} is not fetchable — create did not land")
    engine = json.loads(got.stdout)
    if engine.get("displayName") != AGENT_DISPLAY_NAME:
        die(f"engine {engine_id} has display name {engine.get('displayName')!r}, expected {AGENT_DISPLAY_NAME!r}")
    ok(f"engine {engine_id} is live")

    # 2. The engine carries the expected gateway binding.
    gateway = _find_gateway_binding(engine)
    if gateway is None:
        die("engine spec shows no gateway binding",
            "The engine was created without agentGatewayConfig — check the\n"
            "  agent_gateway_config block in deploy.py.")
    if gateway != GC_AGENT_GATEWAY:
        die(f"engine bound to {gateway}, expected {GC_AGENT_GATEWAY}")
    ok(f"gateway binding: {GC_AGENT_GATEWAY.rsplit('/', 1)[-1]}")

    # 3. The engine's agent principal holds roles/iap.egressor.
    project_number = _gcloud("projects", "describe", GC_PROJECT_ID,
                             "--format=value(projectNumber)")
    principal = (
        f"principal://agents.global.org-{_org_id()}.system.id.goog"
        f"/resources/aiplatform/projects/{project_number}"
        f"/locations/{GC_REGION}/reasoningEngines/{engine_id}"
    )
    policy = json.loads(_gcloud(
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
{LINE}
  ✔ DEPLOY COMPLETE — {AGENT_DISPLAY_NAME}  ({elapsed:.0f}s)
{LINE}
    engine:   {resource_name}
    gateway:  {GC_AGENT_GATEWAY.rsplit('/', 1)[-1]}  (egress live after ~3 min propagation)
{LINE}""")


def _list_engines() -> list:
    out = subprocess.run(
        ["curl", "-s", "-H", f"Authorization: Bearer {_gcloud('auth', 'print-access-token')}",
         f"https://aiplatform.googleapis.com/v1/projects/{GC_PROJECT_ID}/locations/{GC_REGION}/reasoningEngines"],
        capture_output=True, text=True, check=True,
    ).stdout
    return json.loads(out).get("reasoningEngines", [])


def _find_gateway_binding(engine: dict) -> str | None:
    return (
        engine.get("spec", {})
        .get("deploymentSpec", {})
        .get("agentGatewayConfig", {})
        .get("agentToAnywhereConfig", {})
        .get("agentGateway")
    )


if __name__ == "__main__":
    _DEPLOY_T0 = time.monotonic()
    try:
        preflight()
        cleanup_stale_engines()
        resource_name = create_engine()
        grant_egress(resource_name)
        postdeploy(resource_name)
    except KeyboardInterrupt:
        die("interrupted")
