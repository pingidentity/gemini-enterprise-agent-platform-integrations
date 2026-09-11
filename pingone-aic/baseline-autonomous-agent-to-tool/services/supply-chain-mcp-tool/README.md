# Supply Chain MCP Tool

An MCP server on Cloud Run exposing a single `restock` tool. This is the resource the agent ultimately calls. The agent will have to go through the agent gateway (and the agent-gateway-extension-service) to get here. This MCP Server also verifies the OAuth token that the gateway injects.

The `restock` handler itself is a mock that returns a hardcoded accepted order — the demo's point is the auth boundary, not the business logic.

## Configure

### 1. PingOne AIC - no resource needed

AIC has no "resources" — everything the SaaS resource did is now configured elsewhere:

- **Audience (`supply-chain-mcp-tool`)** — the extension's exchange request carries it in the `audience` parameter; AIC unions it onto the token's `aud` array (see the [extension README](../agent-gateway-extension-service/README.md)).
- **Scope (`supply-chain:restock`)** — lives on each client's scope list.
- **`sub` and `act` claims** — AIC stamps both natively on token exchange (from the subject and actor tokens); no attributes or expressions.
- **Delegation enforcement (`may_act`)** — the May Act script on the agent client + AIC's fail-closed exchange validation replaces the SaaS `act` expression. A wrong-actor exchange is rejected at the token endpoint, before any token is minted.

This service only consumes the result: a token whose `aud` array contains `supply-chain-mcp-tool`, `sub` is the agent, and `act.sub` is the extension.

### 2. Configure environment values

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_REGION` | GCP region, e.g. `us-central1` |
| `GC_CLOUD_RUN_SERVICE_NAME` | Cloud Run service name, e.g. `baatt-supply-chain-mcp-tool` |
| `IDP_ISSUER` | AIC issuer — note the explicit `:443` port, it must byte-match the token's `iss` claim: `https://<tenant-id>.forgeblocks.com:443/am/oauth2/realms/root/realms/<realm>` |
| `IDP_REQUIRED_AUDIENCE` | Expected `aud` claim, e.g. `supply-chain-mcp-tool` (AIC unions audiences, so membership is checked, not equality) |
| `IDP_REQUIRED_SCOPE` | Scope the inbound token must carry, e.g. `supply-chain:restock` (AIC serializes `scope` as a JSON array; `hasScope` handles both that and the SaaS string form) |

## Deploy

```bash
make deploy
```

`deploy` runs `setup`, then `push`, then `gcloud run deploy`.

## Register

Register the server in the Agent Registry (Agent Platform → Govern → Agent Registry → Add MCP Server).
- **Name:** BAATT Supply Chain MCP Tool
- **Description:** Simple Restock tool for BAATT demo
- **Region:** Same as Cloud Run deployment (`us-central-1`)
- **MCP Server URL:** `<URL of Cloud Run MCP Server>/mcp`
- **Tool specification JSON:** Paste the contents of `tool-spec.json`

![Supply Chain MCP Tool GCP Config](../../../../_docs/baseline-autonomous-agent-to-tool/supply-chain-mcp-tool-gcp-config.png)
