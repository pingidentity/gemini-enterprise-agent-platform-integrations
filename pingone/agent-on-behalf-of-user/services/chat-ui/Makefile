-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)
SERVICE    := $(GC_CLOUD_RUN_SERVICE_NAME)
IMAGE      := gcr.io/$(PROJECT_ID)/$(SERVICE):latest

.PHONY: build push deploy destroy clean require-env

require-env:
	@gcloud auth list --filter="status:ACTIVE" --format="value(account)" 2>/dev/null | grep -q . || { echo "FAIL auth     no active gcloud account — run gcloud auth login"; exit 1; }
	@test -n "$(GC_REGION)"                 || { echo "GC_REGION not set"; exit 1; }
	@test -n "$(GC_CLOUD_RUN_SERVICE_NAME)" || { echo "GC_CLOUD_RUN_SERVICE_NAME not set"; exit 1; }

build: require-env
	npm install
	npm run build

push: build
	docker build --platform linux/amd64 -t $(IMAGE) .
	docker push $(IMAGE)

deploy: push
	gcloud run deploy $(SERVICE) \
		--image $(IMAGE) \
		--region $(GC_REGION) \
		--allow-unauthenticated --no-invoker-iam-check

destroy:
	@echo "destroy $(SERVICE)"
	@if gcloud run services describe $(SERVICE) --project $(PROJECT_ID) --region $(GC_REGION) >/dev/null 2>&1; then \
		gcloud run services delete $(SERVICE) --project $(PROJECT_ID) --region $(GC_REGION) --quiet >/dev/null \
			&& echo "PASS service  deleted" || { echo "FAIL service  delete"; exit 1; }; \
	else echo "SKIP service  (not found)"; fi
	@if gcloud container images describe $(IMAGE) >/dev/null 2>&1; then \
		gcloud container images delete $(IMAGE) --quiet --force-delete-tags >/dev/null \
			&& echo "PASS image    deleted" || { echo "FAIL image    delete"; exit 1; }; \
	else echo "SKIP image    (not found)"; fi

clean:
	rm -rf dist
