-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)

IMAGE         := gcr.io/$(PROJECT_ID)/$(GC_CLOUD_RUN_SERVICE_NAME):latest
SA            := ext-svc@$(PROJECT_ID).iam.gserviceaccount.com
SECRET        := idp-client-secret
AUTHZ_SECRET  := authz-client-secret

.PHONY: build push setup deploy require-env

require-env:
	@test -n "$(GC_CLOUD_RUN_SERVICE_NAME)" || { echo "GC_CLOUD_RUN_SERVICE_NAME not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(GC_REGION)"                 || { echo "GC_REGION not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_TOKEN_ENDPOINT)"        || { echo "IDP_TOKEN_ENDPOINT not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_CLIENT_ID)"             || { echo "IDP_CLIENT_ID not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_CLIENT_SECRET)"         || { echo "IDP_CLIENT_SECRET not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_SCOPE)"                 || { echo "IDP_SCOPE not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_REQUIRED_AUDIENCE)"     || { echo "IDP_REQUIRED_AUDIENCE not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(TOOL_URL)"                  || { echo "TOOL_URL not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(AUTHZ_DECISION_ENDPOINT)"   || { echo "AUTHZ_DECISION_ENDPOINT not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(AUTHZ_CLIENT_ID)"           || { echo "AUTHZ_CLIENT_ID not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(AUTHZ_CLIENT_SECRET)"       || { echo "AUTHZ_CLIENT_SECRET not set — fill in .env (copy from .env.sample)"; exit 1; }

build:
	go build ./...

push:
	docker build -t $(IMAGE) .
	docker push $(IMAGE)

setup: require-env
	gcloud services enable run.googleapis.com secretmanager.googleapis.com iam.googleapis.com
	@gcloud iam service-accounts describe $(SA) >/dev/null 2>&1 \
		|| gcloud iam service-accounts create ext-svc --display-name "ext-svc"
	@gcloud secrets describe $(SECRET) >/dev/null 2>&1 \
		|| gcloud secrets create $(SECRET) --replication-policy automatic
	@printf '%s' "$(IDP_CLIENT_SECRET)" | gcloud secrets versions add $(SECRET) --data-file=-
	gcloud secrets add-iam-policy-binding $(SECRET) \
		--member "serviceAccount:$(SA)" \
		--role roles/secretmanager.secretAccessor
	@gcloud secrets describe $(AUTHZ_SECRET) >/dev/null 2>&1 \
		|| gcloud secrets create $(AUTHZ_SECRET) --replication-policy automatic
	@printf '%s' "$(AUTHZ_CLIENT_SECRET)" | gcloud secrets versions add $(AUTHZ_SECRET) --data-file=-
	gcloud secrets add-iam-policy-binding $(AUTHZ_SECRET) \
		--member "serviceAccount:$(SA)" \
		--role roles/secretmanager.secretAccessor

deploy: require-env setup push
	gcloud run deploy $(GC_CLOUD_RUN_SERVICE_NAME) \
		--image $(IMAGE) \
		--region $(GC_REGION) \
		--service-account $(SA) \
		--allow-unauthenticated \
		--ingress=all \
		--port 50051 \
		--use-http2 \
		--set-env-vars "GRPC_PORT=50051,IDP_TOKEN_ENDPOINT=$(IDP_TOKEN_ENDPOINT),IDP_CLIENT_ID=$(IDP_CLIENT_ID),IDP_SCOPE=$(IDP_SCOPE),IDP_REQUIRED_AUDIENCE=$(IDP_REQUIRED_AUDIENCE),TOOL_URL=$(TOOL_URL),AUTHZ_DECISION_ENDPOINT=$(AUTHZ_DECISION_ENDPOINT),AUTHZ_CLIENT_ID=$(AUTHZ_CLIENT_ID)" \
		--set-secrets "IDP_CLIENT_SECRET=$(SECRET):latest,AUTHZ_CLIENT_SECRET=$(AUTHZ_SECRET):latest"
