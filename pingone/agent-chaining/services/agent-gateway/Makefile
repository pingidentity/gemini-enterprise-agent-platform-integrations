-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)
EXT_YAML := authz-extension.yaml
POLICY_YAML := authz-policy.yaml

.PHONY: render attach

render:
	@EXT_HOST=$$(gcloud run services describe $(GC_EXT_SVC_NAME) --region $(GC_REGION) --format='value(status.url)' | sed 's|https://||'); \
	export PROJECT_ID GC_REGION GC_GATEWAY_NAME GC_EXT_SVC_NAME EXT_HOST; \
	envsubst < authz-extension.tmpl.yaml > $(EXT_YAML); \
	envsubst < authz-policy.tmpl.yaml > $(POLICY_YAML); \
	echo "Rendered $(EXT_YAML) and $(POLICY_YAML)"

attach: render
	gcloud beta service-extensions authz-extensions import $(GC_AUTHZ_EXTENSION) --source $(EXT_YAML) --location $(GC_REGION)
	gcloud beta network-security authz-policies import $(GC_AUTHZ_POLICY) --source $(POLICY_YAML) --location $(GC_REGION)
