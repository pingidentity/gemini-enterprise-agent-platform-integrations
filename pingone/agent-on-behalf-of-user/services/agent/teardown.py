"""Delete the OBO agent engine by display name."""

import os
import sys

import agentplatform
from dotenv import load_dotenv

load_dotenv()

PROJECT_ID = os.environ["GC_PROJECT_ID"]
REGION = os.environ["GC_REGION"]
DISPLAY_NAME = os.environ["AGENT_DISPLAY_NAME"]

client = agentplatform.Client(project=PROJECT_ID, location=REGION)

engines = [e for e in client.agent_engines.list() if e.api_resource.display_name == DISPLAY_NAME]
if not engines:
    print(f"No agent engine named '{DISPLAY_NAME}' found — nothing to delete.")
    sys.exit(0)

for e in engines:
    name = e.api_resource.name
    print(f"Deleting {name} ...")
    client.agent_engines.delete(name=name, force=True)
    print("Deleted.")
