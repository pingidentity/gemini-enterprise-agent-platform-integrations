-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)

EXT_YAML    := authz-ext.yaml
POLICY_YAML := authz-policy.yaml

.PHONY: attach render require-env clean

require-env:
	@test -n "$(GC_REGION)"           || { echo "GC_REGION not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(GC_GATEWAY_NAME)"     || { echo "GC_GATEWAY_NAME not set — fill in .env"; exit 1; }
	@test -n "$(GC_EXT_SVC_NAME)"     || { echo "GC_EXT_SVC_NAME not set — fill in .env"; exit 1; }
	@test -n "$(GC_AUTHZ_EXTENSION)"  || { echo "GC_AUTHZ_EXTENSION not set — fill in .env"; exit 1; }
	@test -n "$(GC_AUTHZ_POLICY)"     || { echo "GC_AUTHZ_POLICY not set — fill in .env"; exit 1; }

render: require-env
	@EXT_HOST=$$(gcloud run services describe $(GC_EXT_SVC_NAME) --region $(GC_REGION) --format 'value(status.url)' | sed 's|https://||'); \
	test -n "$$EXT_HOST" || { echo "Could not resolve $(GC_EXT_SVC_NAME) URL — is it deployed?"; exit 1; }; \
	export PROJECT_ID GC_REGION GC_GATEWAY_NAME GC_AUTHZ_EXTENSION GC_AUTHZ_POLICY EXT_HOST; \
	envsubst < authz-extension.tmpl.yaml > $(EXT_YAML); \
	envsubst < authz-policy.tmpl.yaml    > $(POLICY_YAML); \
	echo "Rendered $(EXT_YAML) and $(POLICY_YAML)"

attach: render
	gcloud beta service-extensions authz-extensions import $(GC_AUTHZ_EXTENSION) \
		--source $(EXT_YAML) --location $(GC_REGION)
	gcloud beta network-security authz-policies import $(GC_AUTHZ_POLICY) \
		--source $(POLICY_YAML) --location $(GC_REGION)
	@echo "Attached. Verify under the gateway's Service Extensions in the console."

clean:
	rm -f $(EXT_YAML) $(POLICY_YAML)
