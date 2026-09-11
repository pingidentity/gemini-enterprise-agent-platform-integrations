"""CRM agent — ADK agent deployed on Agent Runtime (Agent Engine)."""

import os

from google.adk.agents import Agent
from google.adk.tools.mcp_tool import McpToolset
from google.adk.tools.mcp_tool.mcp_session_manager import StreamableHTTPConnectionParams
from google.genai import types as genai_types

from pingone import mcp_headers

TOOL_MCP_URL = os.environ.get("TOOL_MCP_URL", "")

restock_tool = McpToolset(
    connection_params=StreamableHTTPConnectionParams(url=TOOL_MCP_URL),
    header_provider=mcp_headers,
)

root_agent = Agent(
    model="gemini-2.5-flash",
    name="crm_agent",
    description="CRM agent that restocks inventory via the supply chain MCP tool.",
    instruction=(
        "You are a CRM inventory agent. When asked to restock a product, call "
        "the `restock` tool with the product_id, quantity, and region. Report "
        "the order result back to the user."
    ),
    tools=[restock_tool],
    generate_content_config=genai_types.GenerateContentConfig(
        # thinking_budget=0 disables chain-of-thought — this agent only does
        # single-tool calls so reasoning adds latency with no benefit.
        thinking_config=genai_types.ThinkingConfig(thinking_budget=0),
    ),
)
