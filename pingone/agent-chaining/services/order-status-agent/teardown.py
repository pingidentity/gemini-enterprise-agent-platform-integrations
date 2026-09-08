"""Delete Order Status Agent engines matching AGENT_DISPLAY_NAME."""

import os
from dotenv import load_dotenv
import agentplatform

load_dotenv()
client = agentplatform.Client(project=os.environ["GC_PROJECT_ID"], location=os.environ["GC_REGION"])
for engine in client.agent_engines.list():
    if engine.api_resource.display_name == os.environ["AGENT_DISPLAY_NAME"]:
        print("Deleting Order Status Agent:", engine.api_resource.name)
        client.agent_engines.delete(name=engine.api_resource.name)
