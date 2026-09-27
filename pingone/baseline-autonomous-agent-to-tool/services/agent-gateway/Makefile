-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)

POLICY_YAML := authz-policy.yaml

# The gateway and its /mcp interception policy. The authz-extension resource
# (the ext_proc callout registration) is owned by the extension service — its
# `make deploy` imports it; this Makefile only binds it to the gateway here.
require-env:
	@gcloud auth list --filter="status:ACTIVE" --format="value(account)" 2>/dev/null | grep -q . || { echo "FAIL auth     no active gcloud account — run gcloud auth login"; exit 1; }
	@command -v gcloud >/dev/null || { echo "FAIL gcloud   not on PATH — install the Google Cloud CLI (cloud.google.com/sdk/docs/install)"; exit 1; }; \
	gcloud beta service-extensions --help >/dev/null 2>&1 || { echo "FAIL gcloud   beta service-extensions component missing or CLI stale — run gcloud components update"; exit 1; }
	@test -n "$(GC_REGION)"           || { echo "GC_REGION not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(GC_GATEWAY_NAME)"     || { echo "GC_GATEWAY_NAME not set — fill in .env"; exit 1; }
	@test -n "$(GC_AUTHZ_POLICY_NAME)"     || { echo "GC_AUTHZ_POLICY_NAME not set — fill in .env"; exit 1; }

# Gateway must exist (console-created); the extension resource must already be
# registered (the ext service's `make deploy` does that).
preflight: require-env
	@echo "preflight"
	@gw=$$(gcloud beta network-services agent-gateways describe $(GC_GATEWAY_NAME) \
		--project $(PROJECT_ID) --location $(GC_REGION) --format 'value(name)' 2>/dev/null); \
	test -n "$$gw" || { echo "FAIL gateway $(GC_GATEWAY_NAME) not found in $(PROJECT_ID)/$(GC_REGION)"; \
		echo "  Create it in the console first (Agent Platform → Govern → Gateways → Add gateway,"; \
		echo "  pick the 'Allow Policy (legacy)' policy model) or fix GC_GATEWAY_NAME in .env."; exit 1; }; \
	echo "PASS gateway  $(GC_GATEWAY_NAME) (exists)"
	@ext=$$(gcloud beta service-extensions authz-extensions describe $(GC_SERVICE_EXTENSION_NAME) \
		--project $(PROJECT_ID) --location $(GC_REGION) --format 'value(name)' 2>/dev/null); \
	test -n "$$ext" || { echo "FAIL authz extension $(GC_SERVICE_EXTENSION_NAME) not found"; \
		echo "  The extension service registers itself on deploy:"; \
		echo "  (cd ../agent-gateway-extension-service && make deploy)"; exit 1; }; \
	echo "PASS authzext $(GC_SERVICE_EXTENSION_NAME) (registered)"

render: preflight
	@echo "render"
	@export PROJECT_ID GC_REGION GC_GATEWAY_NAME GC_AUTHZ_POLICY_NAME; \
	envsubst < authz-policy.tmpl.yaml > $(POLICY_YAML); \
	echo "PASS rendered $(POLICY_YAML)"

# Import the interception policy, then verify what the gateway enforces.
attach: render
	@echo "import"
	@out=$$(gcloud beta network-security authz-policies import $(GC_AUTHZ_POLICY_NAME) \
		--source $(POLICY_YAML) --location $(GC_REGION) --project $(PROJECT_ID) 2>&1) \
	|| { echo "FAIL authz policy import"; \
		echo "$$out" | grep -v "^Waiting" | grep -v "^\.*done"; \
		echo "  The policy binds extension $(GC_SERVICE_EXTENSION_NAME) to gateway $(GC_GATEWAY_NAME);"; \
		echo "  an error here usually means the extension resource did not land —"; \
		echo "  redeploy the extension service: (cd ../agent-gateway-extension-service && make deploy)"; exit 1; }; \
	echo "PASS imported authzPolicies/$(GC_AUTHZ_POLICY_NAME)"
	@$(MAKE) verify-attach

# Describe the policy back; assert the fields the gateway enforces.
verify-attach:
	@echo "verify"
	@action=$$(gcloud beta network-security authz-policies describe $(GC_AUTHZ_POLICY_NAME) \
		--location $(GC_REGION) --project $(PROJECT_ID) --format json 2>/dev/null | jq -r '.action // empty'); \
	test "$$action" = "CUSTOM" || { echo "FAIL policy action = $${action:-<none>}, expected CUSTOM"; exit 1; }; \
	prof=$$(gcloud beta network-security authz-policies describe $(GC_AUTHZ_POLICY_NAME) \
		--location $(GC_REGION) --project $(PROJECT_ID) --format json 2>/dev/null | jq -r '.policyProfile // empty'); \
	test "$$prof" = "CONTENT_AUTHZ" || { echo "FAIL policy profile = $${prof:-<none>}, expected CONTENT_AUTHZ (full-duplex body echo contract)"; exit 1; }; \
	echo "PASS policy action=CUSTOM profile=CONTENT_AUTHZ"
	@tgt=$$(gcloud beta network-security authz-policies describe $(GC_AUTHZ_POLICY_NAME) \
		--location $(GC_REGION) --project $(PROJECT_ID) --format json 2>/dev/null | jq -r '.target.resources[0] // empty'); \
	echo "$$tgt" | grep -q "agentGateways/$(GC_GATEWAY_NAME)$$" \
		|| { echo "FAIL policy targets '$$tgt', expected agentGateways/$(GC_GATEWAY_NAME)"; exit 1; }; \
	echo "PASS policy target  = $(GC_GATEWAY_NAME)"
	@extref=$$(gcloud beta network-security authz-policies describe $(GC_AUTHZ_POLICY_NAME) \
		--location $(GC_REGION) --project $(PROJECT_ID) --format json 2>/dev/null | jq -r '.customProvider.authzExtension.resources[0] // empty'); \
	echo "$$extref" | grep -q "authzExtensions/$(GC_SERVICE_EXTENSION_NAME)$$" \
		|| { echo "FAIL policy references '$$extref', expected authzExtensions/$(GC_SERVICE_EXTENSION_NAME)"; exit 1; }; \
	echo "PASS policy ext ref = $(GC_SERVICE_EXTENSION_NAME)"
	@path=$$(gcloud beta network-security authz-policies describe $(GC_AUTHZ_POLICY_NAME) \
		--location $(GC_REGION) --project $(PROJECT_ID) --format json 2>/dev/null | jq -r '.httpRules[0].to.operations[0].paths[0].prefix // empty'); \
	test "$$path" = "/mcp" || { echo "FAIL policy path prefix = '$${path:-<none>}', expected /mcp"; \
		echo "  Authorize only fires on /mcp — check authz-policy.tmpl.yaml."; exit 1; }; \
	echo "PASS policy path    = /mcp"
	@echo ""
	@echo "✔ ATTACH COMPLETE — policy wired to $(GC_GATEWAY_NAME)"
	@echo "  Callouts to the ext service start flowing on the next /mcp egress request."

destroy: require-env
	@echo "destroy gateway resources"
	@for p in $(GC_AUTHZ_POLICY_NAME) $(GC_GATEWAY_NAME)-iap-authzpolicy; do \
		if gcloud beta network-security authz-policies describe $$p --location $(GC_REGION) --project $(PROJECT_ID) >/dev/null 2>&1; then \
			gcloud beta network-security authz-policies delete $$p --location $(GC_REGION) --project $(PROJECT_ID) --quiet >/dev/null \
				&& echo "PASS authz policy  $$p deleted" || { echo "FAIL authz policy  $$p"; exit 1; }; \
		else echo "SKIP authz policy  $$p (not found)"; fi; \
	done
	@e=$(GC_GATEWAY_NAME)-iap-authzextension; \
	if gcloud beta service-extensions authz-extensions describe $$e --location $(GC_REGION) --project $(PROJECT_ID) >/dev/null 2>&1; then \
		gcloud beta service-extensions authz-extensions delete $$e --location $(GC_REGION) --project $(PROJECT_ID) --quiet >/dev/null \
			&& echo "PASS authz ext     $$e deleted" || { echo "FAIL authz ext     $$e"; exit 1; }; \
	else echo "SKIP authz ext     $$e (not found)"; fi
	@if gcloud beta network-services agent-gateways describe $(GC_GATEWAY_NAME) --location $(GC_REGION) --project $(PROJECT_ID) >/dev/null 2>&1; then \
		ok=""; \
		for i in 1 2 3 4 5 6; do \
			if gcloud beta network-services agent-gateways delete $(GC_GATEWAY_NAME) --location $(GC_REGION) --project $(PROJECT_ID) --quiet >/dev/null 2>&1; then \
				ok=1; break; \
			fi; \
			echo "  gateway delete blocked (concurrent op?) — retry $$i/6 in 30s"; \
			sleep 30; \
		done; \
		if [ -n "$$ok" ]; then echo "PASS gateway       $(GC_GATEWAY_NAME) deleted"; \
		else echo "FAIL gateway       delete — another operation is stuck on it"; exit 1; fi; \
	else echo "SKIP gateway       (not found)"; fi
	@$(MAKE) clean

clean:
	rm -f $(POLICY_YAML)

.PHONY: require-env preflight render attach verify-attach destroy clean
