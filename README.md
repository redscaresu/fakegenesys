# fakegenesys

[![CI](https://github.com/redscaresu/fakegenesys/actions/workflows/ci.yml/badge.svg)](https://github.com/redscaresu/fakegenesys/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.25-blue.svg)](go.mod)

Local Go-based mock of the **Genesys Cloud CCaaS** Terraform provider
HTTP surface. Single binary, single process, SQLite-backed state,
deterministic behavior — designed for fast inner-loop feedback when
generating + validating OpenTofu against the `mypurecloud/genesyscloud`
provider.

Sibling repos in the same family:

- [mockway](https://github.com/redscaresu/mockway) — Scaleway mock (`:8080`)
- [fakegcp](https://github.com/redscaresu/fakegcp) — GCP mock (`:8081`)
- [fakeaws](https://github.com/redscaresu/fakeaws) — AWS mock (`:8082`)
- **fakegenesys** — Genesys Cloud CCaaS mock (`:8083`)
- [infrafactory](https://github.com/redscaresu/infrafactory) — the
  meta-repo that drives all four mocks for OpenTofu generation
  validation.

## Quickstart

```bash
git clone https://github.com/redscaresu/fakegenesys.git
cd fakegenesys
go mod download
make install-hooks
make test
make run    # serves the mock at :8083
```

### Driving real terraform/tofu against the mock

The repo ships `make demo-*` targets that wire up the env + drive a real
`mypurecloud/genesyscloud` provider through a full lifecycle against
fakegenesys. Useful for blog demos and manual exploration.

```bash
make build              # one-time
make demo-apply         # boots fakegenesys + init + apply + plan-no-op (auth_role)
make demo-apply EXAMPLE=routing_queue
make demo-shell         # bash subshell with env set + cd'd to example
make demo-help          # full target list + available examples
make demo-down          # kill fakegenesys + clean temp files
```

The `plan -detailed-exitcode == 0` check at the end of `demo-apply` is the
correctness oracle — drift in any wire-shape detail (case-sensitive JSON
keys, exact status codes, default fields) surfaces here.

Mint a token + hit the API:

```bash
TOKEN=$(curl -s -X POST http://localhost:8083/oauth/token \
  -d 'grant_type=client_credentials&client_id=any&client_secret=any' \
  | jq -r .access_token)

curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8083/api/v2/users
```

## API compatibility

fakegenesys speaks the same wire shapes as Genesys Cloud's public REST
API. The contract is validated by `examples/provider_smoke_test.go`,
which runs the real `mypurecloud/genesyscloud` Terraform provider
against every example dir under `examples/{working,misconfigured,updates}/`.
The provider IS the wire-format validator — no real Genesys tenant
needed.

## Testing examples

The canonical entry point for end-to-end example coverage is
`go test ./examples/...`:

```bash
# Run every example end-to-end (apply → plan-no-op → destroy)
FAKEGENESYS_ENABLE_E2E=1 go test ./examples/...

# Run one specific example, with verbose output
FAKEGENESYS_ENABLE_E2E=1 go test ./examples/... -v -run TestProviderSmokeWorking/<dir>

# Filter to a single sub-tree
FAKEGENESYS_ENABLE_E2E=1 go test ./examples/... -run TestProviderSmokeMisconfigured
```

The harness is **self-contained** as of v0.2.1 — it builds the
`fakegenesys` binary, spawns a fresh instance on a random port per
example, AND wires up the `genesyscloud` provider's environment
automatically (`HTTPS_PROXY` routing through the TLS MITM proxy,
`SSL_CERT_FILE` pointing at the boot-time CA, `NO_PROXY` for
external registries, and credentials env vars). No manual setup
required — `go test ./examples/...` runs end-to-end from a fresh
clone.

The per-tree contract:

| Tree | Contract |
|---|---|
| `working/` | `tofu apply` → `tofu plan -detailed-exitcode` (no diff) → `tofu destroy` |
| `misconfigured/` | `tofu apply` MUST fail; if `expected.txt` is present, output MUST contain that fragment |
| `updates/` | apply `v1.tfvars` → no-op plan → apply `v2.tfvars` → no-op plan → destroy |

The same in-test pattern is canonical across all four sibling fakes
([mockway](https://github.com/redscaresu/mockway),
[fakegcp](https://github.com/redscaresu/fakegcp),
[fakeaws](https://github.com/redscaresu/fakeaws)) — `go test
./examples/...` works identically in each.

Wire format:

| Property | Value |
|---|---|
| Auth | OAuth2 `client_credentials` → Bearer token |
| API base | `http://localhost:8083` (configurable via `--port`) |
| Wire shape | REST/JSON; paged lists via `pageNumber` + `pageSize` query |
| Error body | `{status, code, message, contextId}` |
| Default port | `:8083` |

## Fidelity strategy

Spec-driven, mirroring [mockway](https://github.com/redscaresu/mockway).
The committed `specs/genesys-openapi.json` (downloaded from
`https://api.mypurecloud.com/api/v2/docs/swagger`) is the primary
source of truth for handler shapes. The "Reverse fidelity" rule
applies: **never enforce validation the spec doesn't declare.** Detail
in `AGENTS.md` § "Fidelity strategy".

Run `make specs-refresh` to re-download the spec when upgrading to a
newer provider version.

## Project layout

```
cmd/fakegenesys/   CLI entry point
handlers/          chi router, per-resource handlers, OAuth, admin endpoints
models/            shared types + sentinel errors
repository/        SQLite-backed state engine
testutil/          test helpers (per-test fresh server + Bearer token)
examples/          {working,misconfigured,updates}/<resource>/ smoke-harness inputs
specs/             Genesys Cloud OpenAPI spec (Swagger 2.0 JSON)
docs/              review-passes, spec-notes
```

## Contributing

See `CONTRIBUTING.md` for the per-bundle PR rule + the per-resource
ticket pattern. New resources land as one PR with handler + tests +
examples + `coverage_matrix.yaml` update + spec note.

## License

Apache-2.0. See [LICENSE](LICENSE).

## Security

Report security issues via GitHub's private vulnerability reporting.
See [SECURITY.md](SECURITY.md).
