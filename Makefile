.PHONY: build test test-race test-short test-coverage vet clean run install-hooks specs-refresh up

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

# `make run` and `make up` are aliases — same as fakeaws / fakegcp.
run: build
	./fakegenesys --port 8083

up: run

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
