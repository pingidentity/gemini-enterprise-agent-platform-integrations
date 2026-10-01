"""Delete Order Status Agent engines matching AGENT_DISPLAY_NAME."""

import os
import subprocess

from dotenv import load_dotenv
import agentplatform

load_dotenv()

# Same derivation as deploy.py — gcloud is the source of truth on this host.
project_id = os.environ.get("GC_PROJECT_ID")
if not project_id:
    project_id = subprocess.run(
        ["gcloud", "config", "get-value", "project"], capture_output=True, text=True
    ).stdout.strip()
if not project_id:
    raise SystemExit("GC_PROJECT_ID not set and gcloud has no active project")
client = agentplatform.Client(project=project_id, location=os.environ["GC_REGION"])
for engine in client.agent_engines.list():
    if engine.api_resource.display_name == os.environ["AGENT_DISPLAY_NAME"]:
        print("Deleting Order Status Agent:", engine.api_resource.name)
        client.agent_engines.delete(name=engine.api_resource.name)
