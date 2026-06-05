# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added (S109)
- **5 identity resources** with full CRUD: `genesyscloud_user` (POST/GET/list/PATCH/DELETE, **soft delete** flipping `state=deleted`), `genesyscloud_group` (POST/GET/list/PUT/DELETE), `genesyscloud_location` (POST/GET/list/PATCH/DELETE), `genesyscloud_auth_role` (POST/GET/list/PUT+PATCH/DELETE + unique-name 409), `genesyscloud_oauth_client` (POST/GET/list/PUT/DELETE + **reveal-once secret**).
- **Shared CRUD helpers** in `handlers/crud.go`: paged-list envelope, JSON body decode, ID generation, error helpers. Mirrors fakegcp's pattern.
- **SQLite schema**: per-resource tables with the unique constraints the spec declares (`users.email`, `auth_roles.name`).
- **Test coverage**: lifecycle + pagination + 400 + 404 + 409 + soft-delete + reveal-once across all 5 resources (`handlers/identity_test.go`).
- **Examples**: 15 directories (5 resources × {working, updates, misconfigured}). Each working/updates dir is a 2-line resource block; each misconfigured dir exercises a documented error path with `expected.txt`.
- **`/mock/state`** now includes the 5 identity resource arrays via the generic `gatherTable` helper (no per-resource gather code).
- **`coverage_matrix.yaml`** seeded with the 5 identity entries.
- **`docs/spec-notes/identity.md`** with per-endpoint shape + error code summary, cross-referenced to the handlers.

### Added (S108)
- **Repo scaffold + OSS-mature layout** mirroring fakeaws (LICENSE, SECURITY, CONTRIBUTING, CODE_OF_CONDUCT, CHANGELOG, .gitleaks.toml, .githooks/pre-commit, CI + release workflows, dependabot).
- **`cmd/fakegenesys/main.go`** with `--port`, `--db`, `--echo` flags. Default port `:8083` (next after fakeaws `:8082`).
- **OAuth2 client_credentials grant** at `POST /oauth/token`. Issues UUID Bearer tokens with `expires_in=3600`. In-process token store satisfies `repository.Cache` so `/mock/reset` invalidates tokens. Bearer middleware applied to every route except `/oauth/token`, `/mock/*`, and `/healthz`.
- **Admin lifecycle**: `/mock/reset`, `/mock/snapshot`, `/mock/restore`, `/mock/state`, `/mock/state/{service}`. Schema version 1 with per-resource arrays/objects placeholders for S109+/S110+/S111+.
- **`repository/`** — SQLite handle with FK enforcement, `SetMaxOpenConns(1)`, `RegisterCache` for in-process state, Snapshot/Restore via `VACUUM INTO`.
- **`testutil/`** — `NewTestServer(t)` returns a fresh per-test fakegenesys + pre-minted Bearer token. JSON-shaped helper surface (`PostJSON`, `GetJSON`, `PutJSON`, `PatchJSON`, `DeleteJSON`).
- **Provider smoke harness skeleton** (`examples/provider_smoke_test.go`) — auto-discovery walker for `examples/{working,misconfigured,updates}/`, per-test fresh-port fakegenesys spawn, gated by `FAKEGENESYS_ENABLE_E2E=1`. Mirrors fakegcp's per-test pattern.
- **`examples/spec_cross_reference_test.go`** — every implemented route must exist in `specs/genesys-openapi.json`. Skeleton in S108; gets exercised as routes land.
- **Genesys Cloud OpenAPI spec** committed at `specs/genesys-openapi.json` (filtered to fakegenesys-implemented endpoints, ~200KB). `make specs-refresh` re-downloads + re-filters via jq from the full 20MB upstream artifact.
- **AGENTS.md** with architecture diagram, API conventions, fidelity strategy, smoke harness section, per-bundle PR rule, anti-patterns.
- **Makefile** with `build`, `test`, `test-race`, `test-coverage`, `vet`, `run`, `up`, `install-hooks`, `specs-refresh`, `clean`.

### Security
- `.githooks/pre-commit` + `make install-hooks` runs `gitleaks protect --staged` (with strict `.gitleaks.toml` overriding the gitleaks 8.x default allowlist of canonical placeholder secrets) then `go vet`, gofmt, whitespace check, large-file guard, YAML/JSON parse, `go test`.
- `SECURITY.md` with private vulnerability reporting via GitHub Security Advisories.
- Apache-2.0 LICENSE.
