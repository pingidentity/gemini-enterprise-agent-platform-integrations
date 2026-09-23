-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)

EXT_YAML    := authz-ext.yaml
POLICY_YAML := authz-policy.yaml

require-env:
	@gcloud auth list --filter="status:ACTIVE" --format="value(account)" 2>/dev/null | grep -q . || { echo "FAIL auth     no active gcloud account — run gcloud auth login"; exit 1; }
	@command -v gcloud >/dev/null || { echo "FAIL gcloud   not on PATH — install the Google Cloud CLI (cloud.google.com/sdk/docs/install)"; exit 1; }; \
	gcloud beta service-extensions --help >/dev/null 2>&1 || { echo "FAIL gcloud   beta service-extensions component missing or CLI stale — run gcloud components update"; exit 1; }
	@test -n "$(GC_REGION)"           || { echo "GC_REGION not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(GC_GATEWAY_NAME)"     || { echo "GC_GATEWAY_NAME not set — fill in .env"; exit 1; }
	@test -n "$(GC_EXT_SVC_NAME)"     || { echo "GC_EXT_SVC_NAME not set — fill in .env"; exit 1; }
	@test -n "$(GC_AUTHZ_EXTENSION)"  || { echo "GC_AUTHZ_EXTENSION not set — fill in .env"; exit 1; }
	@test -n "$(GC_AUTHZ_POLICY)"     || { echo "GC_AUTHZ_POLICY not set — fill in .env"; exit 1; }

# Prove gateway and ext service exist (and the port is 50051) before any import.
preflight: require-env
	@echo "preflight"
	@gw=$$(gcloud beta network-services agent-gateways describe $(GC_GATEWAY_NAME) \
		--project $(PROJECT_ID) --location $(GC_REGION) --format 'value(name)' 2>/dev/null); \
	test -n "$$gw" || { echo "FAIL gateway $(GC_GATEWAY_NAME) not found in $(PROJECT_ID)/$(GC_REGION)"; \
		echo "  Create it in the console first (Agent Platform → Govern → Gateways → Add gateway,"; \
		echo "  pick the 'Allow Policy (legacy)' policy model) or fix GC_GATEWAY_NAME in .env."; exit 1; }; \
	echo "PASS gateway  $(GC_GATEWAY_NAME) (exists)"
	@port=$$(gcloud run services describe $(GC_EXT_SVC_NAME) --project $(PROJECT_ID) \
		--region $(GC_REGION) --format 'value(spec.template.spec.containers[0].ports[0].containerPort)' 2>/dev/null); \
	test -n "$$port" || { echo "FAIL ext svc  $(GC_EXT_SVC_NAME) not found in $(PROJECT_ID)/$(GC_REGION)"; \
		echo "  Deploy it first: (cd ../agent-gateway-extension-service && make deploy)"; exit 1; }; \
	test "$$port" = "50051" || { echo "FAIL ext svc  container port is $$port, expected 50051 (gRPC ext_proc)"; \
		echo "  The gateway calls ext_proc over gRPC on 50051 — check the Makefile in"; \
		echo "  ../agent-gateway-extension-service."; exit 1; }; \
	echo "PASS ext svc  $(GC_EXT_SVC_NAME) (gRPC :50051)"

# envsubst both templates; EXT_HOST feeds authority: (wrong Host = silent fail closed).
render: preflight
	@echo "render"
	@EXT_HOST=$$(gcloud run services describe $(GC_EXT_SVC_NAME) --project $(PROJECT_ID) \
		--region $(GC_REGION) --format 'value(status.url)' 2>/dev/null | sed 's|https://||'); \
	test -n "$$EXT_HOST" || { echo "FAIL could not resolve $(GC_EXT_SVC_NAME) URL"; exit 1; }; \
	export PROJECT_ID GC_REGION GC_GATEWAY_NAME GC_AUTHZ_EXTENSION GC_AUTHZ_POLICY EXT_HOST; \
	envsubst < authz-extension.tmpl.yaml > $(EXT_YAML); \
	envsubst < authz-policy.tmpl.yaml    > $(POLICY_YAML); \
	grep -q "authority: $$EXT_HOST" $(EXT_YAML) \
		|| { echo "FAIL rendered authz-ext.yaml lost authority: $${EXT_HOST}"; exit 1; }; \
	echo "PASS rendered $(EXT_YAML) + $(POLICY_YAML) (ext host $$EXT_HOST)"

# Import both resources, echoing output only on failure (import prints a wall of wait-dots).
attach: render
	@echo "import"
	@out=$$(gcloud beta service-extensions authz-extensions import $(GC_AUTHZ_EXTENSION) \
		--source $(EXT_YAML) --location $(GC_REGION) --project $(PROJECT_ID) 2>&1) \
	|| { echo "FAIL authz extension import"; \
		echo "$$out" | grep -v "^Waiting" | grep -v "^\.*done"; \
		echo "  If the error mentions the ext service URL, confirm the service is up:"; \
		echo "  gcloud run services describe $(GC_EXT_SVC_NAME) --region $(GC_REGION)"; exit 1; }; \
	echo "PASS imported authzExtensions/$(GC_AUTHZ_EXTENSION)"
	@out=$$(gcloud beta network-security authz-policies import $(GC_AUTHZ_POLICY) \
		--source $(POLICY_YAML) --location $(GC_REGION) --project $(PROJECT_ID) 2>&1) \
	|| { echo "FAIL authz policy import"; \
		echo "$$out" | grep -v "^Waiting" | grep -v "^\.*done"; \
		echo "  The policy binds extension $(GC_AUTHZ_EXTENSION) to gateway $(GC_GATEWAY_NAME);"; \
		echo "  an error here usually means the extension resource above did not land."; exit 1; }; \
	echo "PASS imported authzPolicies/$(GC_AUTHZ_POLICY)"
	@$(MAKE) verify-attach

# Describe both resources back; assert the fields the gateway enforces.
verify-attach:
	@echo "verify"
	@svc=$$(gcloud beta service-extensions authz-extensions describe $(GC_AUTHZ_EXTENSION) \
		--location $(GC_REGION) --project $(PROJECT_ID) --format json 2>/dev/null | jq -r '.service // empty'); \
	test -n "$$svc" || { echo "FAIL authz extension $(GC_AUTHZ_EXTENSION) not found after import"; \
		echo "  The import may have silently no-oped — check the rendered $(EXT_YAML) has an"; \
		echo "  explicit name: field."; exit 1; }; \
	echo "PASS extension service = $$svc"
	@open=$$(gcloud beta service-extensions authz-extensions describe $(GC_AUTHZ_EXTENSION) \
		--location $(GC_REGION) --project $(PROJECT_ID) --format json 2>/dev/null | jq -r '.failOpen // "absent"'); \
	test "$$open" = "absent" || { echo "FAIL failOpen = $$open, expected unset/absent (API default = fail closed)"; exit 1; }; \
	echo "PASS extension failOpen = false (unset — API default)"
	@auth=$$(gcloud beta service-extensions authz-extensions describe $(GC_AUTHZ_EXTENSION) \
		--location $(GC_REGION) --project $(PROJECT_ID) --format json 2>/dev/null | jq -r '.authority // empty'); \
	test -n "$$auth" || { echo "FAIL authority missing on authz extension"; \
		echo "  Without authority: the ext_proc callout sends a wrong Host and fails closed —"; \
		echo "  check authz-extension.tmpl.yaml."; exit 1; }; \
	echo "PASS extension authority = $$auth"
	@action=$$(gcloud beta network-security authz-policies describe $(GC_AUTHZ_POLICY) \
		--location $(GC_REGION) --project $(PROJECT_ID) --format json 2>/dev/null | jq -r '.action // empty'); \
	test "$$action" = "CUSTOM" || { echo "FAIL policy action = $${action:-<none>}, expected CUSTOM"; exit 1; }; \
	prof=$$(gcloud beta network-security authz-policies describe $(GC_AUTHZ_POLICY) \
		--location $(GC_REGION) --project $(PROJECT_ID) --format json 2>/dev/null | jq -r '.policyProfile // empty'); \
	test "$$prof" = "CONTENT_AUTHZ" || { echo "FAIL policy profile = $${prof:-<none>}, expected CONTENT_AUTHZ (full-duplex body echo contract)"; exit 1; }; \
	echo "PASS policy action=CUSTOM profile=CONTENT_AUTHZ"
	@tgt=$$(gcloud beta network-security authz-policies describe $(GC_AUTHZ_POLICY) \
		--location $(GC_REGION) --project $(PROJECT_ID) --format json 2>/dev/null | jq -r '.target.resources[0] // empty'); \
	echo "$$tgt" | grep -q "agentGateways/$(GC_GATEWAY_NAME)$$" \
		|| { echo "FAIL policy targets '$$tgt', expected agentGateways/$(GC_GATEWAY_NAME)"; exit 1; }; \
	echo "PASS policy target  = $(GC_GATEWAY_NAME)"
	@path=$$(gcloud beta network-security authz-policies describe $(GC_AUTHZ_POLICY) \
		--location $(GC_REGION) --project $(PROJECT_ID) --format json 2>/dev/null | jq -r '.httpRules[0].to.operations[0].paths[0].prefix // empty'); \
	test "$$path" = "/mcp" || { echo "FAIL policy path prefix = '$${path:-<none>}', expected /mcp"; \
		echo "  Authorize only fires on /mcp — check authz-policy.tmpl.yaml."; exit 1; }; \
	echo "PASS policy path    = /mcp"
	@echo ""
	@echo "✔ ATTACH COMPLETE — $(GC_AUTHZ_EXTENSION) + $(GC_AUTHZ_POLICY) wired to $(GC_GATEWAY_NAME)"
	@echo "  Callouts to the ext service start flowing on the next /mcp egress request."

destroy: require-env
	@echo "destroy gateway resources"
	@for p in $(GC_AUTHZ_POLICY) $(GC_GATEWAY_NAME)-iap-authzpolicy; do \
		if gcloud beta network-security authz-policies describe $$p --location $(GC_REGION) --project $(PROJECT_ID) >/dev/null 2>&1; then \
			gcloud beta network-security authz-policies delete $$p --location $(GC_REGION) --project $(PROJECT_ID) --quiet >/dev/null \
				&& echo "PASS authz policy  $$p deleted" || { echo "FAIL authz policy  $$p"; exit 1; }; \
		else echo "SKIP authz policy  $$p (not found)"; fi; \
	done
	@for e in $(GC_AUTHZ_EXTENSION) $(GC_GATEWAY_NAME)-iap-authzextension; do \
		if gcloud beta service-extensions authz-extensions describe $$e --location $(GC_REGION) --project $(PROJECT_ID) >/dev/null 2>&1; then \
			gcloud beta service-extensions authz-extensions delete $$e --location $(GC_REGION) --project $(PROJECT_ID) --quiet >/dev/null \
				&& echo "PASS authz ext     $$e deleted" || { echo "FAIL authz ext     $$e"; exit 1; }; \
		else echo "SKIP authz ext     $$e (not found)"; fi; \
	done
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
	rm -f $(EXT_YAML) $(POLICY_YAML)

.PHONY: require-env preflight render attach verify-attach destroy clean
