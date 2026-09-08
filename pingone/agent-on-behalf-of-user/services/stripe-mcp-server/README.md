# Stripe MCP Server

An MCP server exposing Stripe tools. Deployed on Cloud Run, registered as an MCP Server in Agent Registry.

On every request it:
1. Validates `iss`, `aud`, `scope`, and `signature` of the inbound token via PingOne JWKS (token injected by Agent Gateway extension service)
2. Reads the caller's email from the `X-User-Email` header (injected by the Agent Gateway extension service)
3. Executes Stripe operations on behalf of that user

## Configure

### 1. Stripe Setup

In the **Stripe Dashboard**, create the products and customers the agent will operate on. Each customer must have a saved payment method attached to their account so that `create_stripe_payment_intent` can charge them without requiring card details at runtime. The customer email in Stripe must match the user's email in PingOne.

### 2. Create the Stripe MCP Server resource in PingOne

In PingOne, create a **Resource** named `AOBOU Stripe MCP Server` with the `stripe_mcp:invoke` scope and `stripe-mcp-server` audience.

This resource mints the final tool token (the extension's RFC 8693 exchange targets it), so it must prove the full delegation chain: extension → agent → user. On the resource's **Attributes** tab, configure three attributes:

| Attribute | Required | Advanced Expression |
|---|---|---|
| `sub` | no | `${(#root.context.requestData.grantType == "client_credentials") ? "no-subject" : #root.context.requestData.subjectToken.sub}` |
| `act` | yes | `${(#root.context.requestData.grantType == "client_credentials")?"noActor":((#root.context.requestData.subjectToken.may_act.sub == #root.context.requestData.actorToken.client_id)?{"sub":#root.context.requestData.actorToken.client_id,"act":#root.context.requestData.subjectToken.act}:null)}` |
| `grant_type` | no | `${#root.context.requestData.grantType}` |

`sub` must be grant-type-aware: the extension's own `client_credentials` actor token mints on this resource too, and that grant has no `subjectToken`. `act` is Required and nested - it stamps `{"sub": actorToken.client_id, "act": subjectToken.act}` only if the subject token's `may_act.sub` matches the actor token's `client_id`, else `null` fails the exchange. Because this expression carries the subject token's `act` through, the terminal token holds the complete chain (extension → agent → `noActor`). **No `may_act`** - this resource is terminal; nothing ever exchanges further on top of its output.

![Stripe MCP Server Resource Config](../../../../_docs/agent-on-behalf-of-user/pingone/stripe-mcp-server-resource-config.png)

### 3. Create `stripe_customers` group in PingOne

In **Directory → Groups**, create a group called `stripe_customers` and add any PingOne users who have a matching customer record in Stripe (matched by email).

![PingOne Group Config](../../../../_docs/agent-on-behalf-of-user/pingone/group-config.png)

### 4. Configure environment values

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_REGION` | GCP region, e.g. `us-central1` |
| `GC_CLOUD_RUN_SERVICE_NAME` | Cloud Run service name, e.g. `aobou-stripe-mcp-server` |
| `STRIPE_SECRET_KEY` | Stripe secret key (`sk_...`) - stored in Secret Manager as `stripe-secret-key` |
| `IDP_ISSUER` | PingOne issuer URL, e.g. `https://auth.pingone.com/<env-id>/as`. |
| `IDP_REQUIRED_AUDIENCE` | Expected `aud` claim, e.g. `stripe-mcp-server` |
| `IDP_REQUIRED_SCOPE` | Scope the inbound token must carry, e.g. `stripe_mcp:invoke` |

## Deploy

```bash
make deploy
```

`deploy` runs `setup`, then `push`, then `gcloud run deploy`.

## Register

Register the server in the Agent Registry (Agent Platform → Govern → Agent Registry → Add MCP Server):
- **Name:** `aobou-stripe-mcp-server`
- **Description:** Stripe MCP server for the Agent On-Behalf-Of User demo
- **Region:** Same as Cloud Run deployment (e.g. `us-central1`)
- **MCP Server URL:** `<Cloud Run service URL>/mcp`
- **Tool specification JSON:** Paste the contents of `tool-spec.json`

![Stripe MCP Server GCP Config](../../../../_docs/agent-on-behalf-of-user/stripe-mcp-server-gcp-config.png)
