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
| `GC_GATEWAY_NAME` | `baatt-agent-gateway` |
| `GC_EXT_SVC_NAME` | Deployed extension service Cloud Run name |
| `GC_AUTHZ_EXTENSION` / `GC_AUTHZ_POLICY` | Names for the two resources this creates |


## 2. Create the gateway

In the console: **Agent Platform → Govern → Gateways → Add gateway**.

| Field | Value |
|---|---|
| **Name** | `baatt-agent-gateway` |
| **Region** | Same as the two Cloud Run services |
| **Deployment mode** | Google-managed |
| **Governed Access Path** | Agent-to-Anywhere (egress) |
| **Access Authorization** | Enforce policies |

## 3. Attach the extension service

This wires the extension service to the gateway as a `CONTENT_AUTHZ` authorization extension scoped to `/mcp` — two resources: an **authorization extension** (points at your Cloud Run host) and an **authorization policy** (binds that extension to the gateway).

Configure and run:

```bash
make attach
```

`make attach` renders `authz-extension.tmpl.yaml` and `authz-policy.tmpl.yaml`
(filling in your project, region, and the ext-svc's live Cloud Run host), then
imports both with `gcloud`. Run `make render` alone to inspect the generated YAML
without importing. Config:

> **You'll now see two Service Extensions on the gateway — that's expected.**
> They're complementary, not duplicates:
>
> | Extension | Profile | Service | Role |
> |---|---|---|---|
> | `baatt-agent-gateway-iap-authzextension` | `REQUEST_AUTHZ` | `iap.googleapis.com` | Google-managed, **auto-created** with the gateway. Enforces the IAP identity/egress check (`iap.egressor`) — this is the "Auth provider: Google Cloud Identity-Aware Proxy" shown on the gateway. |
> | `baatt-ext-proc-authzext` | `CONTENT_AUTHZ` | your Cloud Run ext-svc | The one you just created. Does the AIC token exchange and `Authorization` injection. |
>
> The IAP extension answers *"is this agent allowed to egress at all?"*; yours
> answers *"mint and inject the tool credential."* Both run on every `/mcp`
> request — leave the IAP one alone.

![Agent Gateway Config](../../../../_docs/baseline-autonomous-agent-to-tool/agent-gateway-config.png)

## 4. Register egress destinations

The gateway governs **all** agent egress, so every host the agent reaches must be
a registered destination in **Agent Platform → Govern → Agent Registry**. Two of
those are already handled:

- **MCP tool** — registered under **MCP Servers** when you deployed it (it's an
  MCP server, not an endpoint).
- **Google APIs** (`aiplatform`, `iamcredentials`, `telemetry` on
  `*.mtls.googleapis.com`) — **auto-created** with the gateway for the runtime's
  own egress. Leave them alone.

So the only endpoints you add here are the two **Ping identity hosts** — under
**Endpoints → Add endpoint**:

- **PingOne AIC** — Destination URL = your AIC tenant (e.g. `https://<tenant-id>.forgeblocks.com`). The agent fetches its token here, and the extension service exchanges tokens here.
- **PingAuthorize** — Destination URL = your PingAuthorize host (e.g. `https://ping-authorize-demo.com`). The extension service sends the decision request here on `tools/call`.

![Agent Gateway Egress Destinations](../../../../_docs/baseline-autonomous-agent-to-tool/agent-gateway-egress-destinations.png)

## 5. Identity-plane setup lives in the client apps, not here

In SaaS, this section configured a PingOne **Resource** (audience + `may_act` attribute) for the gateway. AIC has no resources: the gateway-side token settings moved into the agent client's scripts — the access-token modification script sets `aud=google-cloud-agent-gateway` and the May Act script sets `may_act` naming the extension. Both are part of the agent setup — see [the agent's README](../agent/README.md).
