-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)
SERVICE    := $(GC_CLOUD_RUN_SERVICE_NAME)
IMAGE      := gcr.io/$(PROJECT_ID)/$(SERVICE):latest
SA         := $(SERVICE)@$(PROJECT_ID).iam.gserviceaccount.com

.PHONY: build push setup deploy require-env

require-env:
	@test -n "$(GC_REGION)"                  || { echo "GC_REGION not set"; exit 1; }
	@test -n "$(GC_CLOUD_RUN_SERVICE_NAME)"  || { echo "GC_CLOUD_RUN_SERVICE_NAME not set"; exit 1; }
	@test -n "$(AGENT_ENGINE_NAME)"          || { echo "AGENT_ENGINE_NAME not set"; exit 1; }
	@test -n "$(CORS_ORIGIN)"                || { echo "CORS_ORIGIN not set"; exit 1; }
	@test -n "$(PINGONE_ISSUER)"             || { echo "PINGONE_ISSUER not set"; exit 1; }

push:
	docker build -t $(IMAGE) .
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
		--allow-unauthenticated \
		--set-env-vars "GC_PROJECT_ID=$(GC_PROJECT_ID),GC_REGION=$(GC_REGION),AGENT_ENGINE_NAME=$(AGENT_ENGINE_NAME),CORS_ORIGIN=$(CORS_ORIGIN),PINGONE_ISSUER=$(PINGONE_ISSUER)"
