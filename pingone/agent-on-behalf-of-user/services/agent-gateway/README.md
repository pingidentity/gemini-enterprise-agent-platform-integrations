# Agent Gateway

The Agent Gateway is a **Google-managed** resource. You create it in the console and wire it with `gcloud`. It's the policy enforcement point: all the agent's egress is routed through it, and it calls the [extension service](../agent-gateway-extension-service/README.md) via Envoy [External Processing (`ext_proc`)](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter) on every request.

### 1. Configure environment values

```bash
cp .env.sample .env
make attach
```

| Variable | Value |
|---|---|
| `GC_REGION` | Same region as the gateway and Cloud Run services |
| `GC_GATEWAY_NAME` | `aobou-agent-gateway` |
| `GC_EXT_SVC_NAME` | Deployed extension service Cloud Run name |
| `GC_AUTHZ_EXTENSION` / `GC_AUTHZ_POLICY` | Names for the two resources this creates |

## 2. Create the gateway

In the console: **Agent Platform → Govern → Gateways → Add gateway**.

| Field | Value |
|---|---|
| **Name** | `aobou-agent-gateway` |
| **Region** | Same as the two Cloud Run services |
| **Deployment mode** | Google-managed |
| **Governed Access Path** | Agent-to-Anywhere (egress) |
| **Access Authorization** | Enforce policies |

## 3. Attach the extension service

This wires the extension service to the gateway as a `CONTENT_AUTHZ` authorization extension scoped to `/mcp` - two resources: an **authorization extension** (points at your Cloud Run host) and an **authorization policy** (binds that extension to the gateway).

Configure and run:

```bash
make attach
```

`make attach` renders `authz-extension.tmpl.yaml` and `authz-policy.tmpl.yaml`
(filling in your project, region, and the ext-svc's live Cloud Run host), then
imports both with `gcloud`. Run `make render` alone to inspect the generated YAML
without importing. Config:

> **You'll now see two Service Extensions on the gateway - that's expected.**
> They're complementary, not duplicates:
>
> | Extension | Profile | Service | Role |
> |---|---|---|---|
> | `aobou-agent-gateway-iap-authzextension` | `REQUEST_AUTHZ` | `iap.googleapis.com` | Google-managed, **auto-created** with the gateway. Enforces the IAP identity/egress check (`iap.egressor`) - this is the "Auth provider: Google Cloud Identity-Aware Proxy" shown on the gateway. |
> | `aobou-ext-proc-authzext` | `CONTENT_AUTHZ` | your Cloud Run ext-svc | The one you just created. Does the PingOne token exchange and `Authorization` injection. |
>
> The IAP extension answers *"is this agent allowed to egress at all?"*; yours
> answers *"mint and inject the tool credential."* Both run on every `/mcp`
> request - leave the IAP one alone.

![Agent Gateway Config](../../../../_docs/agent-on-behalf-of-user/agent-gateway-config.png)

## 4. Register egress destinations

In **Agent Platform → Govern → Agent Registry**:

- **Stripe MCP Tool** - registered under **MCP Servers** when you deployed it.
- **PingOne** - under **Endpoints → Add endpoint**, Destination URL = `https://auth.pingone.com` (or your regional variant).
- Google APIs (`*.mtls.googleapis.com`) - auto-created with the gateway. Leave them alone.

The gateway governs **all** agent egress, so every host the agent reaches must be
a registered destination in **Agent Platform → Govern → Agent Registry**. Two of
those are already handled:

- **MCP tool** - registered under **MCP Servers** when you deployed it (it's an
  MCP server, not an endpoint).
- **Google APIs** (`aiplatform`, `iamcredentials`, `telemetry` on
  `*.mtls.googleapis.com`) - **auto-created** with the gateway for the runtime's
  own egress. Leave them alone.

So the only endpoint you add here is **PingOne** - under **Endpoints → Add
endpoint**, Destination URL = your PingOne host (e.g. `https://auth.pingone.ca`).

![Agent Gateway Egress Destinations](../../../../_docs/agent-on-behalf-of-user/agent-gateway-egress-destinations.png)

## 5. Create the PingOne Resource for the gateway

In PingOne, create a **Resource** named `AOBOU Google Cloud Agent Gateway` with the `stripe_mcp:invoke` scope and `google-agent-gateway` audience.

This resource mints the agent's delegated token (the agent's RFC 8693 exchange targets it), so it must prove who delegated to whom and license the extension as the one allowed next actor. On the resource's **Attributes** tab, configure four attributes:

| Attribute | Required | Advanced Expression |
|---|---|---|
| `sub` | no | `${(#root.context.requestData.grantType == "client_credentials") ? "no-subject" : #root.context.requestData.subjectToken.sub}` |
| `act` | yes | `${(#root.context.requestData.grantType == "client_credentials")?"noActor":((#root.context.requestData.subjectToken.may_act.sub == #root.context.requestData.actorToken.client_id)?{"sub":#root.context.requestData.actorToken.client_id,"act":#root.context.requestData.subjectToken.act}:null)}` |
| `may_act` | no | `${{"sub":"<EXT-SVC-CLIENT-ID>"}}` |
| `grant_type` | no | `${#root.context.requestData.grantType}` |

`sub` must be grant-type-aware: the agent's own `client_credentials` actor token (needed before it can act as an exchange actor) mints on this resource too, and that grant has no `subjectToken`. `act` is Required, which is what makes the delegation enforceable - the expression returns `null` (failing the exchange) unless the subject token's `may_act.sub` matches the actor token's `client_id`, and otherwise nests the subject token's own `act` one level deeper. `may_act` is a flat constant licensing the extension service as the sole next actor. `grant_type` makes every token self-describe how it was minted.

![Agent Gateway Resource Config](../../../../_docs/agent-on-behalf-of-user/pingone/agent-gateway-resource-config.png)
