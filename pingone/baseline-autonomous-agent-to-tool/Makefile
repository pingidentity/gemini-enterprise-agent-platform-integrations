S := services

.PHONY: help test destroy clean

help:
	@echo "BAATT journey"
	@echo "  make test      live checks per layer + end-to-end restock"
	@echo "  make destroy   delete everything this journey created on GCP"
	@echo "  make clean     remove local artifacts (venvs, rendered YAMLs)"

# Per-layer checks in dependency order, then an e2e restock via the engine's
# :streamQuery REST API (same call the Python SDK makes). The e2e asserts the
# restock was accepted, so an Authorize DENY (e.g. outside the console's
# business-hours window) fails the suite. gcloud + curl + jq, no venv.
test:
	@gcloud auth list --filter="status:ACTIVE" --format="value(account)" 2>/dev/null | grep -q . || { echo "FAIL auth     no active gcloud account — run gcloud auth login"; exit 1; }
	@command -v gcloud >/dev/null || { echo "FAIL gcloud   not on PATH — install the Google Cloud CLI (cloud.google.com/sdk/docs/install)"; exit 1; }
	@echo "── supply-chain-mcp-tool ──"
	@$(MAKE) -C $(S)/supply-chain-mcp-tool smoke
	@echo ""
	@echo "── agent-gateway-extension-service ──"
	@$(MAKE) -C $(S)/agent-gateway-extension-service verify
	@echo ""
	@echo "── agent-gateway ──"
	@$(MAKE) -C $(S)/agent-gateway verify-attach
	@echo ""
	@echo "── agent ──"
	@pid=$$(grep -E '^GC_PROJECT_ID=' $(S)/agent/.env | cut -d= -f2); \
	region=$$(grep -E '^GC_REGION=' $(S)/agent/.env | cut -d= -f2); \
	display=$$(grep -E '^AGENT_DISPLAY_NAME=' $(S)/agent/.env | cut -d= -f2); \
	test -n "$$pid" && test -n "$$display" || { echo "FAIL $(S)/agent/.env incomplete (GC_PROJECT_ID / AGENT_DISPLAY_NAME)"; exit 1; }; \
	engines=$$(curl -s -H "Authorization: Bearer $$(gcloud auth print-access-token)" \
		"https://aiplatform.googleapis.com/v1/projects/$$pid/locations/$$region/reasoningEngines" \
		| jq '[.reasoningEngines[]? | select(.displayName=="'$$display'")] | length'); \
	test "$$engines" -ge 1 || { echo "FAIL no engine named $$display — run make -C $(S)/agent deploy"; exit 1; }; \
	echo "PASS engine  $$display exists"
	@echo ""
	@echo "── end-to-end ──"
	@pid=$$(grep -E '^GC_PROJECT_ID=' $(S)/agent/.env | cut -d= -f2); \
	region=$$(grep -E '^GC_REGION=' $(S)/agent/.env | cut -d= -f2); \
	display=$$(grep -E '^AGENT_DISPLAY_NAME=' $(S)/agent/.env | cut -d= -f2); \
	tok=$$(gcloud auth print-access-token); \
	engname=$$(curl -s -H "Authorization: Bearer $$tok" \
		"https://aiplatform.googleapis.com/v1/projects/$$pid/locations/$$region/reasoningEngines" \
		| jq -r '[.reasoningEngines[]? | select(.displayName=="'$$display'")][0].name // empty'); \
	test -n "$$engname" || { echo "FAIL e2e     no engine named $$display — run make -C $(S)/agent deploy"; exit 1; }; \
	code=$$(curl -s --max-time 300 -X POST \
		-H "Authorization: Bearer $$tok" \
		-H 'Content-Type: application/json' \
		-d '{"class_method":"stream_query","input":{"message":"Restock 500 units of WIDGET-9000 for us-west-2 using the restock tool.","user_id":"demo"}}' \
		-o /tmp/baatt-e2e.ndjson -w '%{http_code}' \
		"https://$$region-aiplatform.googleapis.com/v1/$$engname:streamQuery"); \
	test "$$code" = 200 || { echo "FAIL e2e     streamQuery HTTP $$code"; head -c 400 /tmp/baatt-e2e.ndjson; echo; exit 1; }; \
	< /tmp/baatt-e2e.ndjson jq -r '(.content.parts // [])[] | if has("function_call") then "  tool_call  \(.function_call.name)(\([.function_call.args | to_entries[] | "\(.key)=\(.value|tostring)"] | join(", ")))" elif has("function_response") then "  tool_resp  \(.function_response.name) -> \(.function_response.response.structuredContent // (.function_response.response.content[0].text // ""))" elif has("text") then "\n\(.text)" else empty end'; \
	calls=$$(jq -s '[.[] | (.content.parts // [])[] | select(.function_call)] | length' /tmp/baatt-e2e.ndjson); \
	accepted=$$(jq -sr '[.[].content.parts[]? | select(has("function_response")) | .function_response.response.structuredContent.status] | join(" ")' /tmp/baatt-e2e.ndjson); \
	test "$$calls" -ge 1 || { echo "FAIL e2e     the model never called the tool — check the extension and Authorize policy (a DENY shows up as text only)"; exit 1; }; \
	test "$$accepted" = "accepted" || { echo "FAIL e2e     restock not accepted (status: $${accepted:-none}) — outside the business-hours window?"; exit 1; }; \
	rm -f /tmp/baatt-e2e.ndjson; \
	echo "PASS e2e     restock accepted"

# Reverse deploy order (engine → gateway resources → Cloud Run services).
destroy:
	@gcloud auth list --filter="status:ACTIVE" --format="value(account)" 2>/dev/null | grep -q . || { echo "FAIL auth     no active gcloud account — run gcloud auth login"; exit 1; }
	@echo "destroy — reverse deployment order"
	@$(MAKE) -C $(S)/agent destroy
	@$(MAKE) -C $(S)/agent-gateway destroy
	@$(MAKE) -C $(S)/agent-gateway-extension-service destroy
	@$(MAKE) -C $(S)/supply-chain-mcp-tool destroy
	@echo ""
	@echo "✔ DESTROY COMPLETE"
	@echo "  kept:   gs://<project>-agent-staging (shared, auto-recreated on deploy)"

clean:
	@find . -name __pycache__ -type d -not -path "*/node_modules/*" -exec rm -rf {} + 2>/dev/null; \
	 rm -f .*.pyc *.pyc 2>/dev/null; true
	$(MAKE) -C $(S)/agent clean
	$(MAKE) -C $(S)/agent-gateway clean
