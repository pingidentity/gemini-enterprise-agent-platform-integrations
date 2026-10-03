import json
import subprocess
import sys
from config import GC_PROJECT_ID, GC_REGION

LINE = "─" * 64


def header(title: str) -> None:
    """Print a stage banner (preflight / deploy / postdeploy)."""
    print(f"\n{LINE}\n  {title}\n{LINE}")


def ok(msg: str) -> None:
    """Print a green check line for a passed step."""
    print(f"  ✔ {msg}")


def die(msg: str, hint: str | None = None) -> None:
    """Print the failure banner and exit non-zero; hint is the fix-it text."""
    print(f"\n  ✗ DEPLOY FAILED — {msg}")
    if hint:
        print(f"\n{hint}")
    sys.exit(1)


def gcloud(*args: str) -> str:
    """Run a gcloud command, return stripped stdout; raise on non-zero exit."""
    return subprocess.run(
        ["gcloud", *args], capture_output=True, text=True, check=True
    ).stdout.strip()


def org_id() -> str:
    """Return this project's GCP organization ID (needed for agent principals)."""
    out = gcloud("projects", "get-ancestors", GC_PROJECT_ID,
                 "--format=value(id,type)")
    for line in out.splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[1] == "organization":
            return parts[0]
    die(f"no organization found for project {GC_PROJECT_ID}")


def engine_url(engine_id: str) -> str:
    """Build the aiplatform REST URL for one Reasoning Engine."""
    return (f"https://aiplatform.googleapis.com/v1/projects/{GC_PROJECT_ID}"
            f"/locations/{GC_REGION}/reasoningEngines/{engine_id}")


def delete_engine(engine_id: str) -> None:
    """Force-delete a Reasoning Engine over REST (no gcloud surface exists)."""
    subprocess.run(
        ["curl", "-s", "-o", "/dev/null", "-X", "DELETE",
         "-H", f"Authorization: Bearer {gcloud('auth', 'print-access-token')}",
         f"{engine_url(engine_id)}?force=true"],
        capture_output=True, text=True, check=True,
    )


def list_engines() -> list:
    """List all Reasoning Engines in project/region (raw dicts from the REST API)."""
    out = subprocess.run(
        ["curl", "-s", "-H", f"Authorization: Bearer {gcloud('auth', 'print-access-token')}",
         f"https://aiplatform.googleapis.com/v1/projects/{GC_PROJECT_ID}/locations/{GC_REGION}/reasoningEngines"],
        capture_output=True, text=True, check=True,
    ).stdout
    return json.loads(out).get("reasoningEngines", [])


def find_gateway_binding(engine: dict) -> str | None:
    """Extract the Agent-to-Anywhere gateway resource from an engine's spec, if bound."""
    return (
        engine.get("spec", {})
        .get("deploymentSpec", {})
        .get("agentGatewayConfig", {})
        .get("agentToAnywhereConfig", {})
        .get("agentGateway")
    )
