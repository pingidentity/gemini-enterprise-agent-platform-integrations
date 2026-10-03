# Support Agent

An ADK agent deployed to **Agent Runtime**. It is the user-facing agent in the agent chain: it delegates order-status requests to the Order Status Agent over A2A and never calls the Order Status MCP Server directly.

The Agent Bridge stores the user's PingOne login token in ADK session state, audienced to this agent's own PingOne resource (`support-agent`). The agent validates it independently (signature, issuer, audience, scope `support-agent:invoke`) before ever using it, then performs its own RFC 8693 exchange — using the user token as the subject and its own PingOne client as the actor — targeting the shared intermediate `ac-google-cloud-agent-gateway` audience. Agent Runtime routes the outbound A2A egress through the Agent Gateway, where the extension service performs the real exchange to the `order-status-agent` audience on top of this one.

## Configure

**1. Create the agent's PingOne application**

- **Name:** AC Support Agent
- **Grant type:** Client Credentials and Token Exchange
- Assign the `ac-google-cloud-agent-gateway` resource so it may request the `order-status:invoke` scope

![Support Agent Resource Config](../../../../_docs/agent-chaining/pingone/support-agent-application-config.png)

**2. Create the agent's PingOne resource**

- **Resource Name / Audience:** `support-agent` (must match `IDP_REQUIRED_AUDIENCE` exactly — it becomes the `aud` claim on the Chat UI's login token)
- **Scope:** `support-agent:invoke`
- **Attributes:**
  - `may_act` — Advanced Expression:
    ```text
    {"sub":"<SUPPORT-AGENT-CLIENT-ID>"}
    ```
  - `grant_type` — Advanced Expression, `Required` left unchecked:
    ```text
    #root.context.requestData.grantType
    ```

![Support Agent Resource Config](../../../../_docs/agent-chaining/pingone/support-agent-resource-config.png)

This resource sees exactly one grant type — `authorization_code` at Chat UI login — so the default User ID mapping for `sub` is all it needs (nothing ever mints `client_credentials` here; this agent's own actor token resolves to the `ac-google-cloud-agent-gateway` resource via its scope grant). This mirrors the AOBOU Financial Agent resource. Deliberately **no `act` attribute**: no actor exists at login, so the login token omits `act` entirely, and the gateway resource's nested expression turns that absence into an explicit `null` at the innermost position of every downstream token. `may_act` is a flat constant licensing Support Agent as the sole actor allowed to exchange this token next (hop 1) - set it to this agent's PingOne client ID.

**3. Fill in `.env`:**

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_REGION` | Deploy region, e.g. `us-central1` (the project ID is derived from gcloud at deploy time) |
| `AGENT_DISPLAY_NAME` | Display name for the Reasoning Engine, e.g. `ac_support_agent` |
| `GC_AGENT_GATEWAY` | Bare gateway name, e.g. `ac-agent-gateway` (the full resource path is built from the derived project ID) |
| `AGENT_ENGINE_ID` | The Order Status Agent's bare Reasoning Engine ID — the mTLS A2A URL is derived from `GC_PROJECT_ID`/`GC_REGION`/this |
| `AGENT_SCOPE` | Scope requested on the delegated A2A token (`order-status:invoke`) |
| `IDP_REQUIRED_AUDIENCE` | Expected `aud` on the inbound browser token (`support-agent`) |
| `IDP_REQUIRED_SCOPE` | Expected scope on the inbound browser token (`support-agent:invoke`) |
| `GATEWAY_AUDIENCE` | Shared intermediate PingOne audience this agent's own exchange targets — must match the gateway extension's config |
| `IDP_ISSUER` | PingOne issuer, e.g. `https://auth.pingone.<region>/<env-id>/as` |
| `AGENT_CLIENT_ID` | Agent's PingOne client ID |
| `AGENT_CLIENT_SECRET` | Agent's PingOne client secret |

## Deploy

```bash
make deploy
```

`deploy.py` creates the Reasoning Engine with `identity_type = AGENT_IDENTITY`, binds it to the gateway, and grants `roles/iap.egressor` on the Agent Registry so the engine can reach all registered endpoints.

![Support Agent Registry Config](../../../../_docs/agent-chaining/support-agent-config.png)
