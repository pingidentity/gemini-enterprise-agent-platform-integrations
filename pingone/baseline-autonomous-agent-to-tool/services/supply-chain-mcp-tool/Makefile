-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)
IMAGE      := gcr.io/$(PROJECT_ID)/$(GC_CLOUD_RUN_SERVICE_NAME):latest
SA         := $(GC_CLOUD_RUN_SERVICE_NAME)@$(PROJECT_ID).iam.gserviceaccount.com

.PHONY: build push setup deploy require-env

require-env:
	@test -n "$(GC_CLOUD_RUN_SERVICE_NAME)" || { echo "GC_CLOUD_RUN_SERVICE_NAME not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(GC_REGION)"                 || { echo "GC_REGION not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_ISSUER)"                || { echo "IDP_ISSUER not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_REQUIRED_AUDIENCE)"     || { echo "IDP_REQUIRED_AUDIENCE not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_REQUIRED_SCOPE)"        || { echo "IDP_REQUIRED_SCOPE not set — fill in .env (copy from .env.sample)"; exit 1; }

build:
	go build ./...

push:
	docker build -t $(IMAGE) .
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
		--allow-unauthenticated \
		--ingress=all \
		--port 8080 \
		--set-env-vars "IDP_ISSUER=$(IDP_ISSUER),IDP_REQUIRED_AUDIENCE=$(IDP_REQUIRED_AUDIENCE),IDP_REQUIRED_SCOPE=$(IDP_REQUIRED_SCOPE)"
