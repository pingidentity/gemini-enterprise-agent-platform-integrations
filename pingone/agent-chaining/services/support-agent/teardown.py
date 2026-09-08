"""Delete Support Agent engines matching AGENT_DISPLAY_NAME."""

import os

from dotenv import load_dotenv
import agentplatform

load_dotenv()
client = agentplatform.Client(project=os.environ["GC_PROJECT_ID"], location=os.environ["GC_REGION"])
name = os.environ["AGENT_DISPLAY_NAME"]

for engine in client.agent_engines.list():
    if engine.api_resource.display_name == name:
        print("Deleting Support Agent:", engine.api_resource.name)
        client.agent_engines.delete(name=engine.api_resource.name, force=True)
