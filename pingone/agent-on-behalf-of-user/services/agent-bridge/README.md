# Agent Bridge

A FastAPI Cloud Run service that acts as the entry point for the Chat UI. It:

1. Validates the user's PingOne token via JWKS (`iss`, `sub`, signature)
2. Creates (or reuses) an ADK session with `user_token` in state
3. Invokes Agent Runtime and streams the response back

The bridge holds no PingOne credentials and performs no token exchange - it simply validates the inbound token and passes it through to the agent via session state.

**Session reuse is per-instance.** The bridge caches each user's ADK session ID in memory (one `sessions.create` call, then reuse). A second bridge instance would create its own session for the same user, losing conversation continuity between them. The stateless alternative - listing sessions per request - was tried and reverted: Agent Runtime's `sessions.list` is quota-limited in this project, and putting it on every chat message failed chats under load. For the demo's single-user, single-instance usage this is a non-issue; production would move the cache to a shared store (Firestore/Memorystore).

## Configure

**1. Fill in `.env`:**

```bash
cp .env.sample .env
```

| Variable | Value |
|---|---|
| `GC_PROJECT_ID` | Target project ID |
| `GC_REGION` | Deploy region, e.g. `us-central1` |
| `GC_CLOUD_RUN_SERVICE_NAME` | Cloud Run service name, e.g. `aobou-agent-bridge` |
| `AGENT_ENGINE_NAME` | Full Reasoning Engine resource name from `make deploy` in the agent directory |
| `CORS_ORIGIN` | Chat UI Cloud Run URL |
| `PINGONE_ISSUER` | `https://auth.pingone.<region>/<env-id>/as` |

## Deploy

```bash
make deploy
```

`deploy` runs `setup`, then `push`, then `gcloud run deploy`.
