"""The financial agent: an ADK agent that buys Stripe products on behalf of a logged-in user.

Every MCP request carries a delegated PingOne token (subject=user, actor=agent —
minted by `pingone.py` from the user's login token in session state). The Agent
Gateway validates it, asks PingOne Authorize, and exchanges it for a tool-scoped
token before the request reaches the Stripe MCP server.
"""

from google.adk.agents import Agent
from google.adk.tools.mcp_tool import McpToolset
from google.adk.tools.mcp_tool.mcp_session_manager import StreamableHTTPConnectionParams
from config import TOOL_MCP_URL
from pingone import mcp_headers


stripe_tool = McpToolset(
    connection_params=StreamableHTTPConnectionParams(url=TOOL_MCP_URL),
    header_provider=mcp_headers,
)

# root_agent is the name ADK's deploy surface looks for — do not rename.
root_agent = Agent(
    model="gemini-2.5-flash",
    name="aobou_financial_agent",
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
