"""The CRM agent: an ADK agent that restocks inventory through the supply-chain MCP tool.

Every MCP request carries the agent's own PingOne token (via `pingone.py`), which
the Agent Gateway's extension service validates, authorizes, and exchanges for a
tool-scoped token before the request reaches the tool.
"""

from google.adk.agents import Agent
from google.adk.tools.mcp_tool import McpToolset
from google.adk.tools.mcp_tool.mcp_session_manager import StreamableHTTPConnectionParams
from google.genai import types as genai_types
from config import TOOL_MCP_URL
from pingone import mcp_headers


restock_tool = McpToolset(
    connection_params=StreamableHTTPConnectionParams(url=TOOL_MCP_URL),
    header_provider=mcp_headers,
)

# root_agent is the name ADK's deploy surface looks for — do not rename.
root_agent = Agent(
    model="gemini-2.5-flash",
    name="baatt_crm_agent",
    description="CRM agent that restocks inventory via the supply chain MCP tool.",
    instruction=(
        "You are a CRM inventory agent. When asked to restock a product, call "
        "the `restock` tool with the product_id, quantity, and region. Report "
        "the order result back to the user."
    ),
    tools=[restock_tool],
    generate_content_config=genai_types.GenerateContentConfig(
        thinking_config=genai_types.ThinkingConfig(thinking_budget=0),
    ),
)
