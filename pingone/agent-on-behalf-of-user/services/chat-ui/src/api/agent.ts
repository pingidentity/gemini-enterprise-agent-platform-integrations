const AGENT_BACKEND_URL = import.meta.env.VITE_AGENT_BRIDGE_URL as string;

/**
 * Sends a user message to the agent bridge. The bridge validates the token,
 * stores it in ADK session state, and invokes Agent Runtime. The agent itself
 * performs the RFC 8693 exchange before calling the MCP tool.
 */
export async function invokeAgent(message: string, subjectToken: string): Promise<string> {
  const response = await fetch(`${AGENT_BACKEND_URL}/chat`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${subjectToken}`,
    },
    body: JSON.stringify({ message }),
  });

  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: `HTTP ${response.status}` }));
    throw new Error(body.error || `Agent request failed: ${response.status}`);
  }

  const { response: agentReply } = await response.json();
  return agentReply;
}
