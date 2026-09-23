-include .env
export

PROJECT_ID ?= $(shell gcloud config get-value project 2>/dev/null)

IMAGE         := gcr.io/$(PROJECT_ID)/$(GC_CLOUD_RUN_SERVICE_NAME):latest
SA            := baatt-ext-svc@$(PROJECT_ID).iam.gserviceaccount.com
SECRET        := baatt-idp-client-secret
AUTHZ_SECRET  := baatt-authz-client-secret

URL := $(shell gcloud run services describe $(GC_CLOUD_RUN_SERVICE_NAME) --project $(PROJECT_ID) --region $(GC_REGION) --format 'value(status.url)' 2>/dev/null)

.PHONY: build push setup deploy destroy verify preflight logs require-env

require-env:
	@gcloud auth list --filter="status:ACTIVE" --format="value(account)" 2>/dev/null | grep -q . || { echo "FAIL auth     no active gcloud account — run gcloud auth login"; exit 1; }
	@command -v gcloud >/dev/null || { echo "FAIL gcloud   not on PATH — install the Google Cloud CLI (cloud.google.com/sdk/docs/install)"; exit 1; }
	@test -n "$(GC_CLOUD_RUN_SERVICE_NAME)" || { echo "GC_CLOUD_RUN_SERVICE_NAME not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(GC_REGION)"                 || { echo "GC_REGION not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_TOKEN_ENDPOINT)"        || { echo "IDP_TOKEN_ENDPOINT not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(EXCHANGE_CLIENT_ID)"        || { echo "EXCHANGE_CLIENT_ID not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(EXCHANGE_CLIENT_SECRET)"    || { echo "EXCHANGE_CLIENT_SECRET not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(TOOL_SCOPE)"                || { echo "TOOL_SCOPE not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_REQUIRED_SCOPE)"        || { echo "IDP_REQUIRED_SCOPE not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(IDP_REQUIRED_AUDIENCE)"     || { echo "IDP_REQUIRED_AUDIENCE not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(TOOL_URL)"                  || { echo "TOOL_URL not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(AUTHZ_DECISION_ENDPOINT)"   || { echo "AUTHZ_DECISION_ENDPOINT not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(AUTHZ_CLIENT_ID)"           || { echo "AUTHZ_CLIENT_ID not set — fill in .env (copy from .env.sample)"; exit 1; }
	@test -n "$(AUTHZ_CLIENT_SECRET)"       || { echo "AUTHZ_CLIENT_SECRET not set — fill in .env (copy from .env.sample)"; exit 1; }

build:
	go build -o baatt-agent-gateway-extension-service .

push:
	docker build --platform linux/amd64 -t $(IMAGE) .
	docker push $(IMAGE)

setup: require-env
	gcloud services enable run.googleapis.com secretmanager.googleapis.com iam.googleapis.com
	@gcloud iam service-accounts describe $(SA) >/dev/null 2>&1 \
		|| gcloud iam service-accounts create baatt-ext-svc --display-name "baatt-ext-svc"
	@gcloud secrets describe $(SECRET) >/dev/null 2>&1 \
		|| gcloud secrets create $(SECRET) --replication-policy automatic
	@printf '%s' "$(EXCHANGE_CLIENT_SECRET)" | gcloud secrets versions add $(SECRET) --data-file=-
	gcloud secrets add-iam-policy-binding $(SECRET) \
		--member "serviceAccount:$(SA)" \
		--role roles/secretmanager.secretAccessor
	@gcloud secrets describe $(AUTHZ_SECRET) >/dev/null 2>&1 \
		|| gcloud secrets create $(AUTHZ_SECRET) --replication-policy automatic
	@printf '%s' "$(AUTHZ_CLIENT_SECRET)" | gcloud secrets versions add $(AUTHZ_SECRET) --data-file=-
	gcloud secrets add-iam-policy-binding $(AUTHZ_SECRET) \
		--member "serviceAccount:$(SA)" \
		--role roles/secretmanager.secretAccessor

deploy: require-env setup
	@$(MAKE) preflight
	@$(MAKE) push
	gcloud run deploy $(GC_CLOUD_RUN_SERVICE_NAME) \
		--image $(IMAGE) \
		--region $(GC_REGION) \
		--service-account $(SA) \
		--allow-unauthenticated --no-invoker-iam-check \
		--ingress=all \
		--port 50051 \
		--use-http2 \
		--set-env-vars "GRPC_PORT=50051,IDP_TOKEN_ENDPOINT=$(IDP_TOKEN_ENDPOINT),EXCHANGE_CLIENT_ID=$(EXCHANGE_CLIENT_ID),TOOL_SCOPE=$(TOOL_SCOPE),IDP_REQUIRED_AUDIENCE=$(IDP_REQUIRED_AUDIENCE),IDP_REQUIRED_SCOPE=$(IDP_REQUIRED_SCOPE),TOOL_URL=$(TOOL_URL),AUTHZ_DECISION_ENDPOINT=$(AUTHZ_DECISION_ENDPOINT),AUTHZ_CLIENT_ID=$(AUTHZ_CLIENT_ID)" \
		--set-secrets "EXCHANGE_CLIENT_SECRET=$(SECRET):latest,AUTHZ_CLIENT_SECRET=$(AUTHZ_SECRET):latest"
	@$(MAKE) verify

# Live checks before any image is pushed: PingOne mints (extension app +
# Authorize worker), JWKS serves keys, decision endpoint answers, tool /health.
# Tokens never printed — only client-id prefixes.
preflight:
	@echo "preflight"
	@test -n "$(PROJECT_ID)" || { echo "FAIL project  gcloud project not set (gcloud config get-value project)"; exit 1; }; \
	echo "PASS project  $(PROJECT_ID) ($(GC_REGION))"
	@code=$$(curl -s -o /dev/null -w '%{http_code}' \
		-u "$(EXCHANGE_CLIENT_ID):$(EXCHANGE_CLIENT_SECRET)" \
		-H 'Content-Type: application/x-www-form-urlencoded' \
		-d grant_type=client_credentials "$(IDP_TOKEN_ENDPOINT)"); \
	test "$$code" = 200 || { echo "FAIL actor    PingOne token mint HTTP $$code"; \
		echo "  400 → wrong EXCHANGE_CLIENT_ID/SECRET (PingOne app: Agent Gateway Extension Application)"; \
		echo "  401 → bad secret"; \
		echo "  403 → Client Credentials grant missing on that app"; exit 1; }; \
	echo "PASS actor    PingOne app $$(printf '%s' "$(EXCHANGE_CLIENT_ID)" | cut -c1-8)… minted (RFC 8693 actor token)"
	@jwks="$$(printf '%s' "$(IDP_TOKEN_ENDPOINT)" | sed 's|/token$$||')/jwks"; \
	code=$$(curl -s -o /tmp/baatt-pf-jwks.json -w '%{http_code}' "$$jwks"); \
	keys=$$(jq '.keys | length' /tmp/baatt-pf-jwks.json 2>/dev/null); \
	rm -f /tmp/baatt-pf-jwks.json; \
	test "$$code" = 200 && test "$$keys" -ge 1 || { echo "FAIL jwks     HTTP $$code, $$keys key(s) at $$jwks"; \
		echo "  Derived from IDP_TOKEN_ENDPOINT (…/as/token → …/as/jwks) — check IDP_TOKEN_ENDPOINT."; exit 1; }; \
	echo "PASS jwks     $$keys signing key(s)"
	@atok=$$(curl -s -u "$(AUTHZ_CLIENT_ID):$(AUTHZ_CLIENT_SECRET)" \
		-H 'Content-Type: application/x-www-form-urlencoded' \
		-d grant_type=client_credentials "$(IDP_TOKEN_ENDPOINT)" | jq -r '.access_token // empty'); \
	test -n "$$atok" || { echo "FAIL authz    worker token mint failed (check AUTHZ_CLIENT_ID/SECRET — PingOne Authorize Worker app)"; exit 1; }; \
	dcode=$$(curl -s -o /tmp/baatt-pf-dec.json -w '%{http_code}' \
		-H "Authorization: Bearer $$atok" -H 'Content-Type: application/json' \
		-d '{"parameters":{"Agent Client ID":"verify-unknown-agent","Request Hour":12}}' \
		"$(AUTHZ_DECISION_ENDPOINT)"); \
	decision=$$(jq -r '.decision // "none"' /tmp/baatt-pf-dec.json 2>/dev/null); \
	rm -f /tmp/baatt-pf-dec.json; \
	test "$$dcode" = 200 || { echo "FAIL authz    decision endpoint HTTP $$dcode"; \
		echo "  401 → worker token rejected; 404 → wrong AUTHZ_DECISION_ENDPOINT id"; \
		echo "  429 → rate limited (latest-version endpoint) — wait a few seconds, re-run"; exit 1; }; \
	echo "PASS authz    worker $$(printf '%s' "$(AUTHZ_CLIENT_ID)" | cut -c1-8)… decision=$$decision (unknown agent)"
	@code=$$(curl -s -o /dev/null -w '%{http_code}' "$(TOOL_URL)/health"); \
	test "$$code" = 200 || { echo "FAIL tool     /health HTTP $$code at $(TOOL_URL)"; \
		echo "  Deploy it first: (cd ../supply-chain-mcp-tool && make deploy)"; \
		echo "  403 → tool stuck private; 404 → wrong TOOL_URL"; exit 1; }; \
	echo "PASS tool     /health 200 — $(TOOL_URL)"
	@echo ""
	@echo "✔ PREFLIGHT PASSED — identity chain and downstream tool reachable"

# Probes the deployed revision over raw ExternalProcessor/Process gRPC
# (hand-encoded protobuf via curl, no grpcurl). Wire shapes, LEN-tagged:
#   ProcessingRequest.request_headers = 2 → 12 | HttpHeaders.headers = 1 → 0a
#   HeaderMap.headers = 1 → 0a | HeaderValue key=1 → 0a, value=2 → 12
#   ProcessingResponse.immediate_response = 7 → 3a; status varints:
#   401 = 089103, 403 = 089303, 200 = 08c801. Passthrough reply starts 0a.
# Contract: no bearer → 401, garbage bearer → 401, other authority → pass.
# Cold starts get 3 tries 2s apart; wrong verdicts never retry.
verify:
	@test -n "$(URL)" || { echo "service $(GC_CLOUD_RUN_SERVICE_NAME) not found — run make deploy first"; exit 1; }
	@echo "verify"
	@echo "PASS endpoint $(URL) (ext_proc gRPC)"
	@GRPC="$(URL)/envoy.service.ext_proc.v3.ExternalProcessor/Process"; \
	TOOL_HOST="$$(printf '%s' "$(TOOL_URL)" | sed -E 's|https?://||; s|/$$||')"; \
	verdict() { \
		case "$$(printf '%s' "$$1" | cut -c11-12)" in \
			3a) case "$$1" in \
				*089103*) echo 401 ;; \
				*089303*) echo 403 ;; \
				*08c801*) echo 200 ;; \
				*) echo imm ;; esac ;; \
			0a) echo pass ;; \
			*) echo bad ;; \
		esac; \
	}; \
	call_grpc() { \
		printf '%s' "$$1" | xxd -r -p | curl -s --http2 \
			-H 'content-type: application/grpc' -H 'te: trailers' \
			--data-binary @- "$$GRPC" | xxd -p | tr -d '\n'; \
	}; \
	varint() { \
		n=$$1; out=""; \
		while [ "$$n" -ge 128 ]; do \
			out="$$(printf '%02x' $$(( $$n % 128 + 128 )))$$out"; \
			n=$$(( $$n / 128 )); \
		done; \
		printf '%s%02x' "$$out" "$$n"; \
	}; \
	hdr_entry() { \
		k_hex=$$(printf '%s' "$$1" | xxd -p | tr -d '\n'); \
		v_hex=$$(printf '%s' "$$2" | xxd -p | tr -d '\n'); \
		k_len=$$(varint $${#1}); v_len=$$(varint $${#2}); \
		kv="0a$${k_len}$${k_hex}12$${v_len}$${v_hex}"; \
		printf '0a%s%s' "$$(varint $$(( $${#kv} / 2 )))" "$$kv"; \
	}; \
	wrap() { \
		printf '%s%s' "$$2" "$$(varint $$(( $${#1} / 2 )))$${1}"; \
	}; \
	build_req() { \
		hdr_map="$$(hdr_entry ':method' POST)$$(hdr_entry ':authority' "$$1")$$(hdr_entry ':path' /mcp)"; \
		if [ -n "$$2" ]; then \
			hdr_map="$$hdr_map$$(hdr_entry authorization "$$2")"; \
		fi; \
		hdrs="$$(wrap "$$hdr_map" 0a)"; \
		pr="$$(wrap "$$hdrs" 12)"; \
		printf '00%08x%s' $$(( $${#pr} / 2 )) "$$pr"; \
	}; \
	probe() { \
		req=$$(build_req "$$2" "$$3"); \
		resp=""; \
		for i in 1 2 3; do \
			resp=$$(call_grpc "$$req"); \
			[ -n "$$resp" ] && break; \
			sleep 2; \
		done; \
		v=$$(verdict "$$resp"); \
		if [ "$$v" = bad ] || [ "$$v" = imm ]; then \
			echo "FAIL $$1 — no/unknown gRPC reply (got '$$v')"; \
			echo "  Is the service up with --port 50051 --use-http2 --ingress=all --allow-unauthenticated?"; \
			echo "  gcloud run services logs tail $(GC_CLOUD_RUN_SERVICE_NAME) --region $(GC_REGION)"; \
			exit 1; \
		fi; \
		if [ "$$4" = 401 ]; then \
			[ "$$v" = 401 ] && { echo "PASS $$1 → 401 immediate (fail closed)"; return 0; }; \
			[ "$$v" = 403 ] && { echo "FAIL $$1 → 403, expected 401"; \
				echo "  A 403 means inbound validation never ran and the exchange failed instead —"; \
				echo "  IDP_REQUIRED_AUDIENCE is probably empty on the deployed revision."; \
				echo "  Compare the service env vars with --set-env-vars in this Makefile; redeploy."; exit 1; }; \
			[ "$$v" = pass ] && { echo "FAIL $$1 was passed through unvalidated"; \
				echo "  TOOL_URL on the deployed revision does not match the tool host, or the"; \
				echo "  revision predates the auth code. Redeploy."; exit 1; }; \
			echo "FAIL $$1 → unexpected status $$v"; exit 1; \
		else \
			[ "$$v" = pass ] && { echo "PASS $$1 → passthrough (only the tool hop is guarded)"; return 0; }; \
			echo "FAIL $$1 was denied ($$v), expected passthrough"; \
			echo "  The extension must pass through authorities other than TOOL_URL."; exit 1; \
		fi; \
	}; \
	probe "no bearer on tool host"      "$$TOOL_HOST" "" "401" && \
	probe "garbage bearer on tool host" "$$TOOL_HOST" "Bearer not-a-real-token" "401" && \
	probe "other authority"             "unrelated.example.com" "Bearer not-a-real-token" "pass" || exit 1; \
	echo ""; \
	echo "✔ VERIFIED — ext_proc is live and fails closed"

logs:
	gcloud run services logs tail $(GC_CLOUD_RUN_SERVICE_NAME) --project $(PROJECT_ID) --region $(GC_REGION)

destroy:
	@echo "destroy $(GC_CLOUD_RUN_SERVICE_NAME)"
	@if gcloud run services describe $(GC_CLOUD_RUN_SERVICE_NAME) --project $(PROJECT_ID) --region $(GC_REGION) >/dev/null 2>&1; then \
		gcloud run services delete $(GC_CLOUD_RUN_SERVICE_NAME) --project $(PROJECT_ID) --region $(GC_REGION) --quiet >/dev/null \
			&& echo "PASS service  deleted" || { echo "FAIL service  delete"; exit 1; }; \
	else echo "SKIP service  (not found)"; fi
	@for s in $(SECRET) $(AUTHZ_SECRET); do \
		if gcloud secrets describe $$s --project $(PROJECT_ID) >/dev/null 2>&1; then \
			gcloud secrets delete $$s --project $(PROJECT_ID) --quiet >/dev/null \
				&& echo "PASS secret   $$s deleted" || { echo "FAIL secret   $$s"; exit 1; }; \
		else echo "SKIP secret   $$s (not found)"; fi; \
	done
	@if gcloud iam service-accounts describe $(SA) --project $(PROJECT_ID) >/dev/null 2>&1; then \
		gcloud iam service-accounts delete $(SA) --project $(PROJECT_ID) --quiet >/dev/null \
			&& echo "PASS sa        deleted" \
			|| { echo "FAIL sa        delete"; exit 1; }; \
	else echo "SKIP sa        (not found)"; fi
	@if gcloud container images describe $(IMAGE) >/dev/null 2>&1; then \
		gcloud container images delete $(IMAGE) --quiet --force-delete-tags >/dev/null \
			&& echo "PASS image    deleted" || { echo "FAIL image    delete"; exit 1; }; \
	else echo "SKIP image    (not found)"; fi
