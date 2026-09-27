# Agent Gateway

The Agent Gateway is a **Google-managed** resource. You create it in the console and wire it with `gcloud`. It's the policy enforcement point: all the agent's egress is routed through it, and it calls the [extension service](../agent-gateway-extension-service/README.md) via Envoy [External Processing (`ext_proc`)](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_proc_filter) on every request.

### 1. Configure environment values

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_REGION` | Same region as the gateway and Cloud Run services |
| `GC_GATEWAY_NAME` | `baatt-agent-gateway` |
| `GC_SERVICE_EXTENSION_NAME` | Name of the Service Extension resource the extension service registered (see its `.env`) |
| `GC_AUTHZ_POLICY_NAME` | Name for the policy resource this creates |


## 2. Create the gateway

In the console: **Agent Platform → Govern → Gateways → Add gateway**.

| Field | Value |
|---|---|
| **Name** | `baatt-agent-gateway` |
| **Region** | Same as the two Cloud Run services |
| **Deployment mode** | Google-managed |
| **Agent registries** | Regional registry |
| **Governed Access Path** | Agent-to-Anywhere (egress) |
| **Access Authorization** | Enforce policies |
| **Policy Model** | Allow Policy |

## 3. Attach the interception policy

Deploying the [extension service](../agent-gateway-extension-service/README.md)
already **registered** its Service Extension resource. This step creates the 
second half: the **authorization policy** that binds that extension to the gateway
and sends it every `/mcp` egress request. The gateway still gates *all* egress on
its own (registry destination allowlist + IAP identity); this policy only decides
which of those allowed requests also get the extension's token remint and PingOne Authorize.


```bash
make attach
```

`make attach` renders `authz-policy.tmpl.yaml` (project, region, gateway name)
and imports it with `gcloud`.

> **You'll now see two Service Extensions on the gateway — that's expected.**
> They're complementary, not duplicates:
>
> | Extension | Profile | Service | Role |
> |---|---|---|---|
> | `baatt-agent-gateway-iap-authzextension` | `REQUEST_AUTHZ` | `iap.googleapis.com` | Google-managed, **auto-created** with the gateway. Enforces the IAP identity/egress check (`iap.egressor`) — this is the "Auth provider: Google Cloud Identity-Aware Proxy" shown on the gateway. |
> | `baatt-agent-gateway-extension` | `CONTENT_AUTHZ` | your Cloud Run ext-svc | The one you just created. Does the PingOne token exchange and `Authorization` injection. |
>
> The IAP extension answers *"is this agent allowed to egress at all?"*; yours
> answers *"mint and inject the tool credential."* Both run on every `/mcp`
> request that passes the registry's destination allowlist — leave the IAP one
> alone.

![Agent Gateway Config](../../../../_docs/baseline-autonomous-agent-to-tool/agent-gateway-config.png)

## 4. Register egress destinations

The gateway governs **all** agent egress: a host the agent reaches must be a
registered destination in **Agent Platform → Govern → Agent Registry**, or the
request is dropped before any policy runs. This is the gate that scopes the
authz policy above — its `/mcp` path rule only selects which *allowed* requests
trigger the extension callout. Two destinations are already handled:

- **MCP tool** — registered under **MCP Servers** when you deployed it (it's an
  MCP server, not an endpoint).
- **Google APIs** (`aiplatform`, `iamcredentials`, `telemetry` on
  `*.mtls.googleapis.com`) — **auto-created** with the gateway for the runtime's
  own egress. Leave them alone.

So the only endpoint you add here is **PingOne** — under **Endpoints → Add
endpoint**, Destination URL = your PingOne host (e.g. `https://auth.pingone.ca`).

![Agent Gateway Egress Destinations](../../../../_docs/baseline-autonomous-agent-to-tool/agent-gateway-egress-destinations.png)

## 5. Create the PingOne Resource for the gateway

In PingOne, create a **Resource** named `BAATT Google Cloud Agent Gateway` with the `supply-chain:restock` scope and `google-cloud-agent-gateway` audience.

This resource mints the agent's subject token, so it must license the extension as the one allowed next actor. On the resource's **Attributes** tab, configure one attribute:

| Attribute | Required | Advanced Expression |
|---|---|---|
| `may_act` | no | `{"sub":"<EXT-SVC-CLIENT-ID>"}` |

`may_act` is a flat constant naming the extension as the sole next actor — this is what the tool resource's `act` check compares against at exchange time. Nothing ever exchanges onto this resource (only `client_credentials` mints), so no `act` attribute is needed here either; the delegation proof lives entirely in the tool resource's `act` mapping, and the agent's identity rides in `client_id`.

The `BAATT Supply Chain MCP Tool` resource needs the matching `act` mapping on its side — see the [supply chain MCP tool's README](../supply-chain-mcp-tool/README.md#1-create-the-supply-chain-mcp-tool-resource-in-pingone).

![Agent Gateway Resource Config](../../../../_docs/baseline-autonomous-agent-to-tool/pingone/agent-gateway-resource-config.png)
