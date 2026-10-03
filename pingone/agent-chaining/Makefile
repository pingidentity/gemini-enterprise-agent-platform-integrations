S := services

.PHONY: help destroy clean

help:
	@echo "AC journey"
	@echo "  make destroy   delete everything this journey created on GCP"
	@echo "  make clean     remove local artifacts (venvs, rendered YAMLs, dist)"

# Reverse deploy order (chat-ui → bridge → support agent → order status agent
# → gateway resources → extension service → MCP server). Google Cloud
# resources only — PingOne config is untouched.
destroy:
	@gcloud auth list --filter="status:ACTIVE" --format="value(account)" 2>/dev/null | grep -q . || { echo "FAIL auth     no active gcloud account — run gcloud auth login"; exit 1; }
	@echo "destroy — reverse deployment order"
	@$(MAKE) -C $(S)/chat-ui destroy
	@$(MAKE) -C $(S)/agent-bridge destroy
	@$(MAKE) -C $(S)/support-agent destroy
	@$(MAKE) -C $(S)/order-status-agent destroy
	@$(MAKE) -C $(S)/agent-gateway destroy
	@$(MAKE) -C $(S)/agent-gateway-extension-service destroy
	@$(MAKE) -C $(S)/order-status-mcp-server destroy
	@echo ""
	@echo "✔ DESTROY COMPLETE"

clean:
	@find . -name __pycache__ -type d -not -path "*/node_modules/*" -exec rm -rf {} + 2>/dev/null; \
	 rm -f .*.pyc *.pyc 2>/dev/null; true
	$(MAKE) -C $(S)/support-agent clean
	$(MAKE) -C $(S)/order-status-agent clean
	$(MAKE) -C $(S)/agent-gateway clean
	$(MAKE) -C $(S)/chat-ui clean
