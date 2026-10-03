VENV := .venv
PY   := $(VENV)/bin/python

-include .env
export

PY3       := $(shell command -v python3.13 2>/dev/null || command -v python3 2>/dev/null)
PY3_VER   := $(shell $(PY3) -c 'import sys; print("%d.%d" % sys.version_info[:2])' 2>/dev/null)
PY3_NUM   := $(shell $(PY3) -c 'import sys; print(sys.version_info[0]*100+sys.version_info[1])' 2>/dev/null)
VENV_VER  := $(shell $(PY) -c 'import sys; print("%d.%d" % sys.version_info[:2])' 2>/dev/null)
VENV_NUM  := $(shell $(PY) -c 'import sys; print(sys.version_info[0]*100+sys.version_info[1])' 2>/dev/null)

PROJECT ?= $(shell gcloud config get-value project 2>/dev/null)

# Vars deploy.py (via config.py) requires from .env — checked before the venv
# step so a half-filled .env fails in seconds, not after pip.
REQUIRED_ENV := GC_REGION AGENT_DISPLAY_NAME GC_AGENT_GATEWAY IDP_ISSUER \
	AGENT_CLIENT_ID AGENT_CLIENT_SECRET TOOL_MCP_URL TOOL_SCOPE GATEWAY_AUDIENCE

.PHONY: require-env venv deploy destroy clean

require-env:
	@gcloud auth list --filter="status:ACTIVE" --format="value(account)" 2>/dev/null | grep -q . || { echo "FAIL auth     no active gcloud account — run gcloud auth login"; exit 1; }
	@test -f .env || { echo "FAIL env      no .env — copy it from .env.sample first"; exit 1; }
	@missing=""; \
	for v in $(REQUIRED_ENV); do \
		val=$$(printenv $$v); \
		[ -n "$$val" ] || missing="$$missing $$v"; \
	done; \
	test -z "$$missing" || { echo "FAIL env      missing:$${missing} — fill in .env (copy from .env.sample)"; exit 1; }; \
	echo "PASS env      all $(words $(REQUIRED_ENV)) required vars set"

venv:
	@if [ -n "$(VENV_NUM)" ] && [ "$(VENV_NUM)" -ge 311 ]; then \
		echo "PASS venv     .venv (Python $(VENV_VER), existing)"; \
	else \
		test -n "$(PY3)" || { echo "FAIL python   no python3 on PATH — install Python >= 3.11 (pyenv, brew, or your distro's package manager)"; exit 1; }; \
		test "$(PY3_NUM)" -ge 311 || { echo "FAIL python   $(PY3) is Python $(PY3_VER), need >= 3.11"; \
			echo "  install newer: pyenv install 3.13 / brew install python@3.13 / apt (deadsnakes PPA)"; exit 1; }; \
		echo "PASS venv     creating .venv (Python $(PY3_VER))"; \
		$(PY3) -m venv $(VENV); \
	fi
	@if [ "$$($(PY) -c 'import sys; print(sys.version_info[0]*100+sys.version_info[1])' 2>/dev/null || echo 0)" -ge 314 ]; then \
		echo "WARN venv     Python >= 3.14 — 3.13 is the validated version; the py3.14 worker base image broke engine workers (see CLAUDE.md)"; \
	fi
	$(PY) -m pip install --quiet --upgrade pip
	$(PY) -m pip install --quiet -r requirements.txt

deploy: require-env venv
	$(PY) deploy.py

destroy:
	@gcloud auth list --filter="status:ACTIVE" --format="value(account)" 2>/dev/null | grep -q . || { echo "FAIL auth     no active gcloud account — run gcloud auth login"; exit 1; }
	@echo "destroy agent"
	@command -v gcloud >/dev/null || { echo "FAIL gcloud   not on PATH — install the Google Cloud CLI (cloud.google.com/sdk/docs/install)"; exit 1; }
	@test -n "$(GC_REGION)"          || { echo "FAIL env      GC_REGION not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(AGENT_DISPLAY_NAME)" || { echo "FAIL env      AGENT_DISPLAY_NAME not set — fill in .env (copy from .env.sample)"; exit 1; }
	@base="https://aiplatform.googleapis.com/v1/projects/$(PROJECT)/locations/$(GC_REGION)/reasoningEngines"; \
	tok=$$(gcloud auth print-access-token); \
	out=$$(curl -s -w '\n%{http_code}' -H "Authorization: Bearer $$tok" "$$base"); \
	code=$$(printf '%s' "$$out" | tail -1); \
	[ "$$code" = 200 ] || { echo "FAIL engine   list HTTP $$code"; exit 1; }; \
	engines=$$(printf '%s' "$$out" | sed '$$d' | jq -r --arg n "$(AGENT_DISPLAY_NAME)" \
		'[.reasoningEngines[]? | select(.displayName == $$n) | .name] | join(" ")'); \
	if [ -z "$$engines" ]; then \
		echo "SKIP engine   none named $(AGENT_DISPLAY_NAME)"; \
	else \
		org=$$(gcloud projects get-ancestors $(PROJECT) --format='value(id,type)' | awk '$$2=="organization" {print $$1}'); \
		[ -n "$$org" ] || { echo "FAIL org      no organization found for the active gcloud project"; exit 1; }; \
		pnum=$$(gcloud projects describe $(PROJECT) --format='value(projectNumber)'); \
		for e in $$engines; do \
			id=$${e##*/}; \
			echo "  · deleting engine $$id …"; \
			principal="principal://agents.global.org-$$org.system.id.goog/resources/aiplatform/projects/$$pnum/locations/$(GC_REGION)/reasoningEngines/$$id"; \
			gcloud alpha iap web remove-iam-policy-binding --resource-type=agent-registry --region=$(GC_REGION) \
				--member="$$principal" --role=roles/iap.egressor --project=$(PROJECT) >/dev/null 2>&1 \
				&& echo "  · egress grant revoked" || echo "  · egress grant not present (skipping)"; \
			dcode=$$(curl -s -o /dev/null -w '%{http_code}' -X DELETE -H "Authorization: Bearer $$tok" \
				"$$base/$$id?force=true"); \
			[ "$$dcode" = 200 ] || [ "$$dcode" = 404 ] || { echo "FAIL engine   delete HTTP $$dcode"; exit 1; }; \
		done; \
		i=0; \
		while [ $$i -lt 20 ]; do \
			i=$$((i+1)); \
			engines=$$(curl -s -H "Authorization: Bearer $$tok" "$$base" \
				| jq -r --arg n "$(AGENT_DISPLAY_NAME)" \
				'[.reasoningEngines[]? | select(.displayName == $$n) | .name] | join(" ")'); \
			[ -z "$$engines" ] && break; \
			sleep 3; \
		done; \
		if [ -n "$$engines" ]; then \
			echo "FAIL engine   did not delete in time — check the console (Agent Platform → Agent Runtime)"; exit 1; \
		fi; \
		echo "PASS destroy  $(AGENT_DISPLAY_NAME)"; \
		echo "  kept: gs://$(PROJECT)-agent-staging (shared, auto-recreated on deploy)"; \
	fi
	@$(MAKE) clean

clean:
	rm -rf $(VENV)
	@find . -name __pycache__ -type d -exec rm -rf {} + 2>/dev/null; \
	 rm -f .*.pyc *.pyc 2>/dev/null; true
