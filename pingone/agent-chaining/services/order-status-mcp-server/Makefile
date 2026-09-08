-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)
IMAGE := gcr.io/$(PROJECT_ID)/$(GC_CLOUD_RUN_SERVICE_NAME):latest

.PHONY: build push deploy require-env

require-env:
	@test -n "$(GC_CLOUD_RUN_SERVICE_NAME)" || { echo "GC_CLOUD_RUN_SERVICE_NAME not set"; exit 1; }
	@test -n "$(GC_REGION)" || { echo "GC_REGION not set"; exit 1; }
	@test -n "$(IDP_ISSUER)" || { echo "IDP_ISSUER not set"; exit 1; }
	@test -n "$(IDP_REQUIRED_SCOPE)" || { echo "IDP_REQUIRED_SCOPE not set"; exit 1; }
	@test -n "$(IDP_REQUIRED_AUDIENCE)" || { echo "IDP_REQUIRED_AUDIENCE not set"; exit 1; }

build:
	go build ./...

push:
	docker build -t $(IMAGE) .
	docker push $(IMAGE)

deploy: require-env push
	gcloud run deploy $(GC_CLOUD_RUN_SERVICE_NAME) \
		--image $(IMAGE) \
		--region $(GC_REGION) \
		--allow-unauthenticated \
		--port 8080 \
		--set-env-vars "IDP_ISSUER=$(IDP_ISSUER),IDP_REQUIRED_SCOPE=$(IDP_REQUIRED_SCOPE),IDP_REQUIRED_AUDIENCE=$(IDP_REQUIRED_AUDIENCE)"
