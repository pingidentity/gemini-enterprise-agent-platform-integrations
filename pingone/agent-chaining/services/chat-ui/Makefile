-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)
SERVICE    := $(GC_CLOUD_RUN_SERVICE_NAME)
IMAGE      := gcr.io/$(PROJECT_ID)/$(SERVICE):latest

.PHONY: build push deploy require-env

require-env:
	@test -n "$(GC_REGION)"                 || { echo "GC_REGION not set"; exit 1; }
	@test -n "$(GC_CLOUD_RUN_SERVICE_NAME)" || { echo "GC_CLOUD_RUN_SERVICE_NAME not set"; exit 1; }

build: require-env
	npm install
	npm run build

push: build
	docker build -t $(IMAGE) .
	docker push $(IMAGE)

deploy: push
	gcloud run deploy $(SERVICE) \
		--image $(IMAGE) \
		--region $(GC_REGION) \
		--allow-unauthenticated
