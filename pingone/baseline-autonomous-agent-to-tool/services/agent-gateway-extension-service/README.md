# Agent Gateway Extension Service

An Envoy `ext_proc` gRPC handler that the Agent Gateway calls on every request on the governed path. Deployed on Cloud Run, registered as a Service Extension.

For requests bound to the supply chain MCP tool it:
1. Validates the agent's delegated token: `iss`, `aud`, and `scope`
2. On `tools/call` requests, calls PingOne Authorize with the agent's identity and the request hour; non-`tools/call` requests (initialize, tools/list) skip Authorize
3. On PERMIT, performs an RFC 8693 exchange to produce a tool-audienced token, then injects it as `Authorization: Bearer` before forwarding the request to the supply chain MCP tool

## Configure

### 1. PingOne Authorize - Trust Framework

In **Authorization → Trust Framework**, create the two attributes the extension sends with every decision request. **The attribute Name is the wire contract**: the decision request's parameter keys must byte-match the attribute Name exactly (case-sensitive). The "Resolver Name" shown under each attribute's resolver is a label only — it is NOT matched against request parameters (verified live 2026-09-08 in the agent-chaining journey: sending the resolver name instead of the attribute Name produced `MISSING_ATTRIBUTE` and every decision evaluated wrong).

| Attribute Name (exact) | Type | Sent by extension as parameter key |
|---|---|---|
| `Agent Client ID` | String | `"Agent Client ID"` |
| `Request Hour` | Number | `"Request Hour"` |

The extension sends these as `{"parameters": {"Agent Client ID": ..., "Request Hour": <hour>}}` on the decision endpoint — flat body, no `decisionRequest` envelope (the envelope the console's Test tab generates is not accepted by the decision endpoint).

### 2. PingOne Authorize - Policies

In **Authorization → Policies**, create a Policy Set named `BAATT Agent Gateway Policies` with combining algorithm **DenyOverrides** (`Unless one decision is deny, the decision will be permit`). Under DenyOverrides, unmatched requests default to permit — so the set is deny rules only:

- `Deny other agents` — when `Agent Client ID` is not the CRM agent's client ID
- `Deny outside business hours` — when `Request Hour` is not between 8 and 17 (Pacific)

![PingOne Authorize Policies](../../../../_docs/baseline-autonomous-agent-to-tool/pingone/authorize-policies.png)

### 3. PingOne Authorize - Publish and grab decision endpoint

Go to **Authorization → Version History** and publish the latest version.

Note the decision endpoint URL from **Authorization → Decision Endpoints**.

### 4. PingOne Authorize - Worker App

Create a **Worker** application in PingOne:
- **Name:** BAATT PingOne Authorize Worker App
- **Grant type:** Client Credentials
- **Roles:** Grant `Environment Admin` scoped to this environment

![PingOne Authorize Client Application Config](../../../../_docs/baseline-autonomous-agent-to-tool/pingone/authorize-application-config.png)

### 5. PingOne Token Exchange - OIDC Web App App

Create an **OIDC Web App application** in PingOne
- **Name:** BAATT Agent Gateway Extension
- **Grant Types:** enable both **Client Credentials** and **Token Exchange**
- Assign it the `supply-chain-mcp-tool` resource so it may request the `supply-chain:restock` scope

![Token Exchange Application Config](../../../../_docs/baseline-autonomous-agent-to-tool/pingone/exchange-application-config.png)

## Configure environment values

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_REGION` | Deploy region, e.g. `us-central1` |
| `GC_CLOUD_RUN_SERVICE_NAME` | `baatt-agent-gateway-extension-service` |
| `IDP_TOKEN_ENDPOINT` | `https://auth.pingone.<region>/<env-id>/as/token` |
| `IDP_CLIENT_ID` | Token-exchange worker app Client ID |
| `IDP_CLIENT_SECRET` | Token-exchange worker app Client Secret |
| `IDP_SCOPE` | Scope the inbound token must carry, e.g. `supply-chain:restock` |
| `IDP_REQUIRED_AUDIENCE` | Expected `aud` on the inbound token, e.g. `supply-chain-mcp-tool` |
| `TOOL_URL` | The MCP tool's Cloud Run base URL |
| `AUTHZ_DECISION_ENDPOINT` | PingOne Authorize decision endpoint URL |
| `AUTHZ_CLIENT_ID` | Authorize worker app Client ID |
| `AUTHZ_CLIENT_SECRET` | Authorize worker app Client Secret |

## Deploy

```bash
make deploy
```

`deploy` runs `setup`, then `push`, then `gcloud run deploy`.
