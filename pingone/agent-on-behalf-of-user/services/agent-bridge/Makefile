-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)
SERVICE    := $(GC_CLOUD_RUN_SERVICE_NAME)
IMAGE      := gcr.io/$(PROJECT_ID)/$(SERVICE):latest
SA         := $(SERVICE)@$(PROJECT_ID).iam.gserviceaccount.com

.PHONY: build push setup deploy destroy clean require-env

require-env:
	@gcloud auth list --filter="status:ACTIVE" --format="value(account)" 2>/dev/null | grep -q . || { echo "FAIL auth     no active gcloud account — run gcloud auth login"; exit 1; }
	@test -n "$(GC_REGION)"                  || { echo "GC_REGION not set"; exit 1; }
	@test -n "$(GC_CLOUD_RUN_SERVICE_NAME)"  || { echo "GC_CLOUD_RUN_SERVICE_NAME not set"; exit 1; }
	@test -n "$(AGENT_ENGINE_ID)"            || { echo "AGENT_ENGINE_ID not set"; exit 1; }
	@test -n "$(CORS_ORIGIN)"                || { echo "CORS_ORIGIN not set"; exit 1; }
	@test -n "$(IDP_ISSUER)"                 || { echo "IDP_ISSUER not set"; exit 1; }

push:
	docker build --platform linux/amd64 -t $(IMAGE) .
	docker push $(IMAGE)

setup: require-env
	gcloud services enable run.googleapis.com iam.googleapis.com
	@gcloud iam service-accounts describe $(SA) >/dev/null 2>&1 \
		|| gcloud iam service-accounts create $(SERVICE) --display-name "OBO Agent Bridge"
	gcloud projects add-iam-policy-binding $(GC_PROJECT_ID) \
		--member "serviceAccount:$(SA)" --role roles/aiplatform.user

deploy: require-env setup push
	gcloud run deploy $(SERVICE) \
		--image $(IMAGE) \
		--region $(GC_REGION) \
		--service-account $(SA) \
		--allow-unauthenticated --no-invoker-iam-check \
		--set-env-vars "GC_PROJECT_ID=$(GC_PROJECT_ID),GC_REGION=$(GC_REGION),AGENT_ENGINE_ID=$(AGENT_ENGINE_ID),CORS_ORIGIN=$(CORS_ORIGIN),IDP_ISSUER=$(IDP_ISSUER)"

destroy:
	@$(MAKE) clean
	@echo "destroy $(SERVICE)"
	@if gcloud run services describe $(SERVICE) --project $(PROJECT_ID) --region $(GC_REGION) >/dev/null 2>&1; then \
		gcloud run services delete $(SERVICE) --project $(PROJECT_ID) --region $(GC_REGION) --quiet >/dev/null \
			&& echo "PASS service  deleted" || { echo "FAIL service  delete"; exit 1; }; \
	else echo "SKIP service  (not found)"; fi
	@if gcloud iam service-accounts describe $(SA) --project $(PROJECT_ID) >/dev/null 2>&1; then \
		gcloud projects remove-iam-policy-binding $(GC_PROJECT_ID) \
			--member "serviceAccount:$(SA)" --role roles/aiplatform.user --quiet >/dev/null 2>&1; \
		gcloud iam service-accounts delete $(SA) --project $(PROJECT_ID) --quiet >/dev/null \
			&& echo "PASS sa        deleted" || { echo "FAIL sa        delete"; exit 1; }; \
	else echo "SKIP sa        (not found)"; fi
	@if gcloud container images describe $(IMAGE) >/dev/null 2>&1; then \
		gcloud container images delete $(IMAGE) --quiet --force-delete-tags >/dev/null \
			&& echo "PASS image    deleted" || { echo "FAIL image    delete"; exit 1; }; \
	else echo "SKIP image    (not found)"; fi

clean:
	@find . -name __pycache__ -type d -not -path "*/node_modules/*" -exec rm -rf {} + 2>/dev/null; \
	 rm -f .*.pyc *.pyc 2>/dev/null; true
