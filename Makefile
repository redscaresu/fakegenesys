.PHONY: build test test-race test-short test-coverage vet clean run install-hooks specs-refresh

# install-hooks wires the tracked hook installer at .githooks/ via
# core.hooksPath. Idempotent — re-running is a no-op. Run once after
# cloning. README quickstart calls this out as the second step after
# `go mod download`.
install-hooks:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-commit
	@echo "Hooks installed: pre-commit will run gitleaks then go test."

build:
	go build -o fakegenesys ./cmd/fakegenesys

test:
	go test -count=1 ./...

test-race:
	go test -count=1 -race ./...

test-short:
	go test -count=1 -short ./...

# Aggregate handlers/... coverage.
test-coverage:
	go test -count=1 -coverprofile=cov.out -covermode=atomic ./handlers/...
	@go tool cover -func=cov.out | tail -1
	@go tool cover -html=cov.out -o coverage.html
	@echo "coverage report: coverage.html"

vet:
	go vet ./...

clean:
	rm -f fakegenesys cov.out coverage.html

run: build
	./fakegenesys --port 8083

# specs-refresh re-downloads the Genesys Cloud OpenAPI spec and filters
# it down to the endpoints fakegenesys implements (identity / routing /
# architect / responsemanagement / IDP). The full Genesys spec is 20MB+
# and covers 2000+ endpoints — filtering keeps the committed artifact
# under 250KB and the spec_cross_reference test focused on what we
# actually serve. Per AGENTS.md § "Fidelity strategy".
specs-refresh:
	@echo "Refreshing Genesys Cloud OpenAPI spec..."
	@curl -sSfL -o /tmp/genesys-openapi-full.json https://api.mypurecloud.com/api/v2/docs/swagger
	@echo "Filtering to fakegenesys-implemented endpoints..."
	@jq '{swagger: .swagger, info: .info, host: .host, basePath: .basePath, paths: (.paths | to_entries | map(select(.key | test("^/api/v2/(users|groups|locations|authorization/(roles|subjects)|oauth/clients|routing/(queues|skills|wrapupcodes|languages|utilization)|flows|architect/(datatables|prompts)|responsemanagement/responses|identityproviders/generic|tokens/me)"))) | map({key: .key, value: (.value | with_entries(select(.key | test("^(get|put|post|delete|patch|options|head)$"))) | with_entries(.value |= {operationId, summary, parameters: (.parameters // [] | map({name, in, required, type, schema}))}))}) | from_entries)}' /tmp/genesys-openapi-full.json > specs/genesys-openapi.json
	@echo "Spec refreshed (filtered). Run \`go test ./examples/... -run TestSpecCrossReference\` to verify."

# ----- demo targets -----
#
# Drive the real `mypurecloud/genesyscloud` terraform/tofu provider
# against this fakegenesys. Useful for blog demos, manual exploration,
# and showing the "real provider as oracle" check end-to-end.
#
# Quick start:
#   make demo-apply                       # one-shot: up + apply + plan-no-op (default: auth_role)
#   make demo-apply EXAMPLE=routing_queue # pick a different example
#   make demo-shell                       # bash subshell with env set + cd'd to example
#   make demo-down                        # kill fakegenesys + remove temp files
#
# Override the example with EXAMPLE=<dir> (any subdir of examples/working/).
.PHONY: demo-help demo-up demo-down demo-env demo-shell demo-apply demo-destroy demo-clean

DEMO_PORT      ?= 8083
DEMO_TLS_PORT  ?= 8443
EXAMPLE        ?= auth_role
DEMO_EXAMPLE_DIR := examples/working/$(EXAMPLE)
DEMO_CA_FILE   := /tmp/fakegenesys-ca.pem
DEMO_ENV_FILE  := /tmp/fakegenesys.env
DEMO_BASE      := http://localhost:$(DEMO_PORT)
DEMO_BIN       := $(shell command -v tofu 2>/dev/null || command -v terraform 2>/dev/null)

demo-help:
	@echo "Demo targets (drive real terraform/tofu against this fakegenesys):"
	@echo "  demo-up                        boot fakegenesys + write env to /tmp"
	@echo "  demo-apply [EXAMPLE=<dir>]     one-shot: init + apply + plan-no-op"
	@echo "  demo-shell [EXAMPLE=<dir>]     bash subshell with env set + cd'd to example"
	@echo "  demo-destroy [EXAMPLE=<dir>]   tofu destroy on the current example"
	@echo "  demo-down                      kill fakegenesys + remove temp files"
	@echo "  demo-clean                     demo-destroy + nuke .terraform/ + state files"
	@echo ""
	@echo "Available examples:"
	@ls examples/working/ | sed 's/^/  /'

demo-up:
	@if pgrep -f "fakegenesys --port $(DEMO_PORT)" >/dev/null 2>&1; then \
	  echo "✓ fakegenesys already running on :$(DEMO_PORT)"; \
	else \
	  [ -x ./fakegenesys ] || { echo "ERROR: ./fakegenesys binary not found. Run 'make build' first." >&2; exit 1; }; \
	  ./fakegenesys --port $(DEMO_PORT) --db ':memory:' >/tmp/fakegenesys.log 2>&1 & \
	  for i in 1 2 3 4 5 6 7 8 9 10; do sleep 0.5; curl -sf $(DEMO_BASE)/healthz >/dev/null 2>&1 && break; done; \
	  echo "✓ fakegenesys booted on :$(DEMO_PORT)  (logs: /tmp/fakegenesys.log)"; \
	fi
	@curl -fsS $(DEMO_BASE)/mock/ca-cert -o $(DEMO_CA_FILE)
	@{ \
	  echo 'export GENESYSCLOUD_OAUTHCLIENT_ID=any-client-id'; \
	  echo 'export GENESYSCLOUD_OAUTHCLIENT_SECRET=any-client-secret'; \
	  echo 'export GENESYSCLOUD_REGION=us-east-1'; \
	  echo 'export HTTPS_PROXY=http://localhost:$(DEMO_TLS_PORT)'; \
	  echo 'export HTTP_PROXY=http://localhost:$(DEMO_TLS_PORT)'; \
	  echo 'export NO_PROXY=registry.opentofu.org,registry.terraform.io,releases.hashicorp.com,github.com,127.0.0.1,localhost,.opentofu.org,.terraform.io,.hashicorp.com,.amazonaws.com,.githubusercontent.com,.github.com,.windows.net'; \
	  echo 'export no_proxy="$$NO_PROXY"'; \
	  echo 'export SSL_CERT_FILE=$(DEMO_CA_FILE)'; \
	  echo 'export FAKEGENESYS_UPLOAD_HOST=localhost:$(DEMO_PORT)'; \
	} > $(DEMO_ENV_FILE)
	@echo "✓ env written to $(DEMO_ENV_FILE)"

demo-down:
	@pkill -f "fakegenesys --port $(DEMO_PORT)" 2>/dev/null && echo "✓ killed" || echo "✓ nothing to kill"
	@rm -f $(DEMO_CA_FILE) $(DEMO_ENV_FILE)

demo-env: demo-up
	@cat $(DEMO_ENV_FILE)

demo-shell: demo-up
	@[ -d "$(DEMO_EXAMPLE_DIR)" ] || { echo "ERROR: $(DEMO_EXAMPLE_DIR) not found" >&2; exit 1; }
	@echo "→ entering subshell with fakegenesys env. Type 'exit' to leave."
	@cd $(DEMO_EXAMPLE_DIR) && /bin/bash --rcfile <(echo "source ~/.bashrc 2>/dev/null; source $(DEMO_ENV_FILE); PS1='[fakegenesys $(EXAMPLE)] $$PS1'")

demo-apply: demo-up
	@[ -n "$(DEMO_BIN)" ] || { echo "ERROR: neither tofu nor terraform on PATH" >&2; exit 1; }
	@[ -d "$(DEMO_EXAMPLE_DIR)" ] || { echo "ERROR: $(DEMO_EXAMPLE_DIR) not found" >&2; exit 1; }
	@set -e; . $(DEMO_ENV_FILE); cd $(DEMO_EXAMPLE_DIR); \
	  echo "=== $(DEMO_BIN) init ==="; $(DEMO_BIN) init -input=false; \
	  echo ""; echo "=== $(DEMO_BIN) apply ==="; $(DEMO_BIN) apply -auto-approve -input=false; \
	  echo ""; echo "=== $(DEMO_BIN) plan -detailed-exitcode (brutal correctness check) ==="; \
	  if $(DEMO_BIN) plan -detailed-exitcode -input=false >/dev/null 2>&1; then \
	    echo "✓ exit 0 — wire shape correct (real provider's state matches fakegenesys's responses)."; \
	  else \
	    echo "✗ exit $$? — drift detected."; exit 1; \
	  fi

demo-destroy:
	@[ -n "$(DEMO_BIN)" ] || { echo "ERROR: neither tofu nor terraform on PATH" >&2; exit 1; }
	@[ -f $(DEMO_ENV_FILE) ] || { echo "ERROR: no env file — run 'make demo-up' first" >&2; exit 1; }
	@set -e; . $(DEMO_ENV_FILE); cd $(DEMO_EXAMPLE_DIR); $(DEMO_BIN) destroy -auto-approve -input=false

demo-clean:
	@-$(MAKE) demo-destroy 2>/dev/null
	@find examples/working -name '.terraform' -type d -prune -exec rm -rf {} + 2>/dev/null || true
	@find examples/working -name '.terraform.lock.hcl' -delete 2>/dev/null || true
	@find examples/working -name 'terraform.tfstate*' -delete 2>/dev/null || true
	@echo "✓ terraform state cleaned"
