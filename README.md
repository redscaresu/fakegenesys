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
