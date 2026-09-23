S := services

.PHONY: help destroy clean

help:
	@echo "AOBOU journey"
	@echo "  make destroy   delete everything this journey created on GCP"
	@echo "  make clean     remove local artifacts (venvs, rendered YAMLs)"

# Reverse deploy order (engine → gateway resources → the four Cloud Run
# services). Google Cloud resources only — PingOne config is untouched.
destroy:
	@gcloud auth list --filter="status:ACTIVE" --format="value(account)" 2>/dev/null | grep -q . || { echo "FAIL auth     no active gcloud account — run gcloud auth login"; exit 1; }
	@echo "destroy — reverse deployment order"
	@$(MAKE) -C $(S)/agent destroy
	@$(MAKE) -C $(S)/agent-gateway destroy
	@$(MAKE) -C $(S)/agent-gateway-extension-service destroy
	@$(MAKE) -C $(S)/stripe-mcp-server destroy
	@$(MAKE) -C $(S)/agent-bridge destroy
	@$(MAKE) -C $(S)/chat-ui destroy
	@echo ""
	@echo "✔ DESTROY COMPLETE"
	@echo "  kept: stripe-secret-key secret (shared, not journey-specific)"

clean:
	@find . -name __pycache__ -type d -not -path "*/node_modules/*" -exec rm -rf {} + 2>/dev/null; \
	 rm -f .*.pyc *.pyc 2>/dev/null; true
	$(MAKE) -C $(S)/agent clean
	$(MAKE) -C $(S)/agent-gateway clean
	$(MAKE) -C $(S)/agent-bridge clean
	$(MAKE) -C $(S)/chat-ui clean
