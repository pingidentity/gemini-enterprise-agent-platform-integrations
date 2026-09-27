-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)
IMAGE        := gcr.io/$(PROJECT_ID)/$(GC_CLOUD_RUN_SERVICE_NAME):latest
SA           := ac-ext-svc@$(PROJECT_ID).iam.gserviceaccount.com
SECRET       := ac-idp-client-secret
AUTHZ_SECRET := ac-authz-client-secret

.PHONY: build push setup deploy require-env

require-env:
	@gcloud auth list --filter="status:ACTIVE" --format="value(account)" 2>/dev/null | grep -q . || { echo "FAIL auth     no active gcloud account — run gcloud auth login"; exit 1; }
	@command -v gcloud >/dev/null || { echo "FAIL gcloud   not on PATH — install the Google Cloud CLI (cloud.google.com/sdk/docs/install)"; exit 1; }; \
	command -v docker >/dev/null || { echo "FAIL docker   not on PATH — install it (docs.docker.com/get-docker)"; exit 1; }
	@test -n "$(GC_CLOUD_RUN_SERVICE_NAME)" || { echo "GC_CLOUD_RUN_SERVICE_NAME not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(GC_REGION)"                 || { echo "GC_REGION not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_ISSUER)"        || { echo "IDP_ISSUER not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(EXCHANGE_CLIENT_ID)"        || { echo "EXCHANGE_CLIENT_ID not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(EXCHANGE_CLIENT_SECRET)"    || { echo "EXCHANGE_CLIENT_SECRET not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_REQUIRED_AUDIENCE)"     || { echo "IDP_REQUIRED_AUDIENCE not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_REQUIRED_SCOPE_AGENT)"      || { echo "IDP_REQUIRED_SCOPE_AGENT not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_REQUIRED_SCOPE_TOOL)"       || { echo "IDP_REQUIRED_SCOPE_TOOL not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(AGENT_ENGINE_ID)"           || { echo "AGENT_ENGINE_ID not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(AGENT_AUDIENCE)"            || { echo "AGENT_AUDIENCE not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(AGENT_SCOPE)"               || { echo "AGENT_SCOPE not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(TOOL_URL)"                  || { echo "TOOL_URL not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(TOOL_AUDIENCE)"             || { echo "TOOL_AUDIENCE not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(TOOL_SCOPE)"                || { echo "TOOL_SCOPE not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(AUTHZ_DECISION_ENDPOINT)"   || { echo "AUTHZ_DECISION_ENDPOINT not set — PingOne Authorize is mandatory (no bypass mode)"; exit 1; }
	@test -n "$(AUTHZ_CLIENT_ID)"           || { echo "AUTHZ_CLIENT_ID not set — PingOne Authorize worker app client id required"; exit 1; }
	@test -n "$(AUTHZ_CLIENT_SECRET)"       || { echo "AUTHZ_CLIENT_SECRET not set — PingOne Authorize worker app secret required"; exit 1; }

build:
	go build ./...

push:
	docker build --platform linux/amd64 -t $(IMAGE) .
	docker push $(IMAGE)

setup: require-env
	gcloud services enable run.googleapis.com secretmanager.googleapis.com iam.googleapis.com
	@gcloud iam service-accounts describe $(SA) >/dev/null 2>&1 || gcloud iam service-accounts create ac-ext-svc --display-name "AC Extension Service"
	@gcloud secrets describe $(SECRET) >/dev/null 2>&1 || gcloud secrets create $(SECRET) --replication-policy automatic
	@printf '%s' "$(EXCHANGE_CLIENT_SECRET)" | gcloud secrets versions add $(SECRET) --data-file=-
	gcloud secrets add-iam-policy-binding $(SECRET) --member "serviceAccount:$(SA)" --role roles/secretmanager.secretAccessor
	@gcloud secrets describe $(AUTHZ_SECRET) >/dev/null 2>&1 || gcloud secrets create $(AUTHZ_SECRET) --replication-policy automatic
	@printf '%s' "$(AUTHZ_CLIENT_SECRET)" | gcloud secrets versions add $(AUTHZ_SECRET) --data-file=-
	gcloud secrets add-iam-policy-binding $(AUTHZ_SECRET) --member "serviceAccount:$(SA)" --role roles/secretmanager.secretAccessor

deploy: require-env setup push
	gcloud run deploy $(GC_CLOUD_RUN_SERVICE_NAME) \
		--image $(IMAGE) --region $(GC_REGION) --service-account $(SA) \
		--allow-unauthenticated --no-invoker-iam-check --ingress=all --port 50051 --use-http2 \
		--set-env-vars "GRPC_PORT=50051,GC_REGION=$(GC_REGION),IDP_ISSUER=$(IDP_ISSUER),EXCHANGE_CLIENT_ID=$(EXCHANGE_CLIENT_ID),IDP_REQUIRED_AUDIENCE=$(IDP_REQUIRED_AUDIENCE),IDP_REQUIRED_SCOPE_AGENT=$(IDP_REQUIRED_SCOPE_AGENT),IDP_REQUIRED_SCOPE_TOOL=$(IDP_REQUIRED_SCOPE_TOOL),AGENT_ENGINE_ID=$(AGENT_ENGINE_ID),AGENT_AUDIENCE=$(AGENT_AUDIENCE),AGENT_SCOPE=$(AGENT_SCOPE),TOOL_URL=$(TOOL_URL),TOOL_AUDIENCE=$(TOOL_AUDIENCE),TOOL_SCOPE=$(TOOL_SCOPE),AUTHZ_DECISION_ENDPOINT=$(AUTHZ_DECISION_ENDPOINT),AUTHZ_CLIENT_ID=$(AUTHZ_CLIENT_ID)" \
		--set-secrets "EXCHANGE_CLIENT_SECRET=$(SECRET):latest,AUTHZ_CLIENT_SECRET=$(AUTHZ_SECRET):latest"
