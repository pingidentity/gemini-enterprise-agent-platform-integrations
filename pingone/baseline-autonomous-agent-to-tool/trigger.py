"""Trigger the deployed CRM agent. Run from the baseline root:

    services/agent/.venv/bin/python trigger.py
"""

import os

import vertexai
from vertexai import agent_engines

PROJECT_ID = os.environ.get("GC_PROJECT_ID", "your-project-id")
LOCATION = os.environ.get("GC_REGION", "us-central1")
DISPLAY_NAME = os.environ.get("AGENT_DISPLAY_NAME", "baatt-crm-agent")

vertexai.init(project=PROJECT_ID, location=LOCATION)

matches = list(agent_engines.list(filter=f'display_name="{DISPLAY_NAME}"'))
if not matches:
    raise SystemExit(f"No agent named {DISPLAY_NAME!r} — run `make deploy` first.")
if len(matches) > 1:
    print(f"WARNING: {len(matches)} agents named {DISPLAY_NAME!r}; using the first.")

agent = matches[0]
print(f"Agent: {agent.resource_name}\n")

for event in agent.stream_query(
    message="Restock 500 units of WIDGET-9000 for us-west-2 using the restock tool.",
    user_id="demo",
):
    content = event.get("content", {})
    for part in content.get("parts", []):
        if "function_call" in part:
            fc = part["function_call"]
            args = fc.get("args", {})
            print(f"  tool_call  {fc['name']}({', '.join(f'{k}={v!r}' for k, v in args.items())})")
        elif "function_response" in part:
            fr = part["function_response"]
            structured = fr.get("response", {}).get("structuredContent")
            text = fr.get("response", {}).get("content", [{}])[0].get("text", "")
            print(f"  tool_resp  {fr['name']} -> {structured or text}")
        elif "text" in part:
            print(f"\n{part['text']}")

    finish = event.get("finish_reason")
    if finish and finish != "STOP":
        print(f"\n[finish_reason: {finish}]")
        if err := event.get("error_message", ""):
            print(f"[error: {err}]")
