"""OBO agent — ADK agent deployed on Agent Runtime (Agent Engine)."""

import os

from google.adk.agents import Agent
from google.adk.tools.mcp_tool import McpToolset
from google.adk.tools.mcp_tool.mcp_session_manager import StreamableHTTPConnectionParams
from pingone import mcp_headers

TOOL_MCP_URL = os.environ.get("TOOL_MCP_URL", "")

stripe_tool = McpToolset(
    connection_params=StreamableHTTPConnectionParams(url=TOOL_MCP_URL),
    header_provider=mcp_headers,
)

root_agent = Agent(
    model="gemini-2.5-flash",
    name="obo_agent",
    description="Financial agent that purchases Stripe products on behalf of an authenticated user.",
    instruction=(
        "You are a financial agent acting on behalf of an authenticated user. "
        "You have access to these tools: list_stripe_products (browse the catalog), "
        "get_stripe_product (details on a specific product), get_stripe_customer "
        "(retrieve the user's saved card on file), and create_stripe_payment_intent "
        "(complete a purchase). "
        "When the user wants to browse or list products, call list_stripe_products. "
        "When asked to purchase a product, first call get_stripe_customer to retrieve "
        "and confirm the user's card on file, then call create_stripe_payment_intent to "
        "complete the purchase. Always confirm purchase details with the user before "
        "calling create_stripe_payment_intent."
    ),
    tools=[stripe_tool],
)
