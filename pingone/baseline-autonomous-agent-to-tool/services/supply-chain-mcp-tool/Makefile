-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)
IMAGE      := gcr.io/$(PROJECT_ID)/$(GC_CLOUD_RUN_SERVICE_NAME):latest
SA         := $(GC_CLOUD_RUN_SERVICE_NAME)@$(PROJECT_ID).iam.gserviceaccount.com

.PHONY: build push setup deploy destroy logs smoke require-env

require-env:
	@gcloud auth list --filter="status:ACTIVE" --format="value(account)" 2>/dev/null | grep -q . || { echo "FAIL auth     no active gcloud account — run gcloud auth login"; exit 1; }
	@command -v gcloud >/dev/null || { echo "FAIL gcloud   not on PATH — install the Google Cloud CLI (cloud.google.com/sdk/docs/install)"; exit 1; }; \
	command -v docker >/dev/null || { echo "FAIL docker   not on PATH — install it (docs.docker.com/get-docker)"; exit 1; }
	@test -n "$(GC_CLOUD_RUN_SERVICE_NAME)" || { echo "GC_CLOUD_RUN_SERVICE_NAME not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(GC_REGION)"                 || { echo "GC_REGION not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_ISSUER)"                || { echo "IDP_ISSUER not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_REQUIRED_AUDIENCE)"     || { echo "IDP_REQUIRED_AUDIENCE not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_REQUIRED_SCOPE)"        || { echo "IDP_REQUIRED_SCOPE not set — fill in .env (copy from .env.sample)"; exit 1; }

build:
	go build -o baatt-supply-chain-mcp-tool .

push:
	docker build --platform linux/amd64 -t $(IMAGE) .
	docker push $(IMAGE)

setup: require-env
	gcloud services enable run.googleapis.com iam.googleapis.com
	@gcloud iam service-accounts describe $(SA) >/dev/null 2>&1 \
		|| gcloud iam service-accounts create $(GC_CLOUD_RUN_SERVICE_NAME) --display-name "$(GC_CLOUD_RUN_SERVICE_NAME)"

deploy: require-env setup push
	gcloud run deploy $(GC_CLOUD_RUN_SERVICE_NAME) \
		--image $(IMAGE) \
		--region $(GC_REGION) \
		--service-account $(SA) \
		--allow-unauthenticated --no-invoker-iam-check \
		--ingress=all \
		--port 8080 \
		--set-env-vars "IDP_ISSUER=$(IDP_ISSUER),IDP_REQUIRED_AUDIENCE=$(IDP_REQUIRED_AUDIENCE),IDP_REQUIRED_SCOPE=$(IDP_REQUIRED_SCOPE)"
	@$(MAKE) smoke

URL := $(shell gcloud run services describe $(GC_CLOUD_RUN_SERVICE_NAME) --project $(PROJECT_ID) --region $(GC_REGION) --format 'value(status.url)' 2>/dev/null)

logs:
	gcloud run services logs tail $(GC_CLOUD_RUN_SERVICE_NAME) --project $(PROJECT_ID) --region $(GC_REGION)

destroy: require-env
	@echo "destroy $(GC_CLOUD_RUN_SERVICE_NAME)"
	@if gcloud run services describe $(GC_CLOUD_RUN_SERVICE_NAME) --project $(PROJECT_ID) --region $(GC_REGION) >/dev/null 2>&1; then \
		gcloud run services delete $(GC_CLOUD_RUN_SERVICE_NAME) --project $(PROJECT_ID) --region $(GC_REGION) --quiet >/dev/null \
			&& echo "PASS service  deleted" || { echo "FAIL service  delete"; exit 1; }; \
	else echo "SKIP service  (not found)"; fi
	@if gcloud iam service-accounts describe $(SA) --project $(PROJECT_ID) >/dev/null 2>&1; then \
		gcloud iam service-accounts delete $(SA) --project $(PROJECT_ID) --quiet >/dev/null \
			&& echo "PASS sa        deleted" || { echo "FAIL sa        delete"; exit 1; }; \
	else echo "SKIP sa        (not found)"; fi
	@if gcloud container images describe $(IMAGE) >/dev/null 2>&1; then \
		gcloud container images delete $(IMAGE) --quiet --force-delete-tags >/dev/null \
			&& echo "PASS image    deleted" || { echo "FAIL image    delete"; exit 1; }; \
	else echo "SKIP image    (not found)"; fi

smoke:
	@test -n "$(URL)" || { echo "service $(GC_CLOUD_RUN_SERVICE_NAME) not found — run make deploy first"; exit 1; }
	@echo "smoke $(URL)/health — expect 200"
	@code=$$(curl -s -o /dev/null -w '%{http_code}' $(URL)/health); \
		test "$$code" = "200" || { echo "FAIL: /health returned $$code (expect 200 — stuck private?)"; exit 1; }; \
		echo "PASS 200"
	@echo "smoke $(URL)/mcp — expect 401"
	@code=$$(curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -d '{}' $(URL)/mcp); \
		test "$$code" = "401" || { echo "FAIL: /mcp returned $$code (expect 401 — auth boundary not rejecting?)"; exit 1; }; \
		echo "PASS 401"
