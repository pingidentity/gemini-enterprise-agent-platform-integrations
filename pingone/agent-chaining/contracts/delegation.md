# Delegation contract

This demo illustrates standard OAuth 2.0 RFC 8693 per-hop delegation. The local runtime models the resulting bearer-token claims with a `local-rfc8693.*` token. Production uses a real PingOne token exchange; this document is not a custom JAG specification.

## Required context

```text
user_sub
source_agent
target_agent
hop_type
action
order_id
delegation_id
request_hash
expires_at
```

The leading space in `target_agent` above is intentionally not part of the field name; implementations must use `target_agent`.

## Rules

- A token issued for Order Status Agent is not valid at the Order Status MCP Server.
- A token issued for the Order Status MCP Server is not valid at Order Status Agent.
- The source and target are derived from validated token claims and gateway configuration, not trusted request headers.
- The request hash binds the delegation to the order ID and requested action.
- Delegation expires quickly and is rejected after expiry.
- Policy errors and token exchange failures deny the hop.
