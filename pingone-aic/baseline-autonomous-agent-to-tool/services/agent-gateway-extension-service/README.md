# Agent Gateway Extension Service

An Envoy `ext_proc` gRPC handler that the Agent Gateway calls on every request on the governed path. Deployed on Cloud Run, registered as a Service Extension.

For requests bound to the supply chain MCP tool it:
1. Validates the agent's delegated token: `iss`, `aud`, and `scope`
2. On `tools/call` requests, calls PingAuthorize with the agent's identity and the request hour; non-`tools/call` requests (initialize, tools/list) skip the decision call
3. On an authorised decision, performs an RFC 8693 exchange to produce a tool-audienced token, then injects it as `Authorization: Bearer` before forwarding the request to the supply chain MCP tool

## Configure

### 1. PingAuthorize - Deployed policy package and decision endpoint

In the PingAuthorize Policy Editor, assemble the baatt policies and deploy them to the governance-engine decision endpoint. The extension POSTs a decision request like:

```json
{
  "domain": "baatt",
  "action": "restock",
  "service": "Supply Chain MCP Tool",
  "attributes": {
    "Agent Client ID": "<agent-client-id>",
    "Request Hour": 14
  }
}
```

The `attributes` keys must match what the deployed policy conditions read — `Agent Client ID` and `Request Hour` (agent allow-list + business hours, same policy logic as the SaaS version of this journey). PingAuthorize responds with `"authorised": true/false`; the extension treats anything but an explicit `authorised: true` (with `status.code: OKAY`) as a DENY — fail closed.

**Client authentication for the decision endpoint:** PingAuthorize is configured with a shared secret. Every decision request must carry:

```text
CLIENT-TOKEN: <shared-secret>
```

Requests with a missing or wrong secret get `401 Unauthorized` (`Shared secret does not match`). The secret becomes `AUTHZ_SHARED_SECRET` in this service's `.env` (stored in Secret Manager at deploy time).

No JWT access-token validator is needed on the PingAuthorize side: the extension extracts the agent's `client_id` from the inbound AIC token itself and sends it as a plain request attribute, so PingAuthorize only has to authenticate the extension (shared secret) and evaluate the policy.

![PingAuthorize Policies](../../../../_docs/baseline-autonomous-agent-to-tool/pingone-aic/pingauthorize-policies.png)

### 2. PingOne AIC - Token Exchange setup

Three pieces: two realm-level settings, then the extension's Service app.

**A. Realm level — enable token exchange**

In **Authorization → OAuth2 Provider → Advanced**:

- Add **Token Exchange** to the grant types. AIC rejects every exchange with `Unsupported Grant Type` until this is set — the client-level grant checkbox is necessary but not sufficient.
- Check **Accept Audience Parameters in Token Exchange Requests**. Without it, the `audience` parameter on exchange requests is silently ignored and the tool token comes out audienced to the exchanging client instead of the MCP tool.

**B. Create the extension's Service app**

In PingOne AIC, create a **Service** application:

- **Name:** BAATT Agent Gateway Extension
- **Description:** The GCP Agent Gateway's ext_proc extension service. Fetches its own client-credentials token as actor and exchanges the CRM agent's token for a tool-audienced token on every `tools/call`.
- **Owners:** App Owner
- **Client ID:** `baatt-agent-gateway-extension`
- **Client Secret:** your chosen secret
- **Grant Types:** enable both **Client Credentials** and **Token Exchange**
- **Scopes:** `supply-chain:restock`

**C. Allow the exchange audience**

In **Native Consoles → Access Management → Realms → <realm> → Applications → OAuth 2.0 → Clients → baatt-agent-gateway-extension → Advanced**:

- **Allowed Resource Server Audience Values:** `supply-chain-mcp-tool`

This pairs with the provider-level audience acceptance in step A: AIC unions the requested audience onto the token's `aud` array (the actor's own ID stays in the list — validators must check *membership*, not equality).

The resulting exchange (verified live 2026-09-11) — agent token in, tool token out:

```json
{
  "sub": "baatt-crm-agent",
  "aud": ["baatt-agent-gateway-extension", "supply-chain-mcp-tool"],
  "scope": ["supply-chain:restock"],
  "grant_type": "urn:ietf:params:oauth:grant-type:token-exchange",
  "act": { "sub": "baatt-agent-gateway-extension" }
}
```

AIC stamps `sub` (carried from the subject token) and `act` (from the actor token) natively — no expressions or extra scripts needed, unlike PingOne SaaS where both required resource attributes. Exchange requests whose actor's `client_id` does not match the subject token's `may_act.sub` are rejected (`Invalid token exchange`) — the delegation license is enforced at the token endpoint.

## Configure environment values

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_REGION` | Deploy region, e.g. `us-central1` |
| `GC_CLOUD_RUN_SERVICE_NAME` | `baatt-agent-gateway-extension-service` |
| `IDP_TOKEN_ENDPOINT` | AIC token endpoint: `https://<tenant-id>.forgeblocks.com/am/oauth2/realms/root/realms/<realm>/access_token` |
| `IDP_CLIENT_ID` | `baatt-agent-gateway-extension` |
| `IDP_CLIENT_SECRET` | Extension Service app Client Secret |
| `IDP_SCOPE` | Scope the inbound token must carry, e.g. `supply-chain:restock` |
| `IDP_ISSUER` | Exact `iss` claim of the inbound agent token — AIC includes the port: `https://<tenant-id>.forgeblocks.com:443/am/oauth2/realms/root/realms/<realm>` |
| `IDP_REQUIRED_AUDIENCE` | Expected `aud` on the inbound agent token, e.g. `google-cloud-agent-gateway` |
| `TOOL_AUDIENCE` | Audience requested on the exchanged tool token, e.g. `supply-chain-mcp-tool` (AIC takes it from the `audience` exchange parameter) |
| `TOOL_URL` | The MCP tool's Cloud Run base URL |
| `AUTHZ_DECISION_ENDPOINT` | PingAuthorize governance-engine decision endpoint URL |
| `AUTHZ_SHARED_SECRET` | The shared secret PingAuthorize's decision endpoint expects in the `CLIENT-TOKEN` header |

## Deploy

```bash
make deploy
```

`deploy` runs `setup`, then `push`, then `gcloud run deploy`.
