# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed / Fixed (S112)
- **Restore corruption guard**: `repository.Restore` now stages the snapshot at `<dbPath>.restore-tmp` and opens + verifies before closing the existing handle. On any error path the old DB stays usable instead of leaving a dead handle (full archive in `docs/review-passes/pass1.md` finding #1).
- **spec_cross_reference test rewritten**: walks the live chi route tree via `chi.Walk` and asserts every `/api/v2/...` route exists in `specs/genesys-openapi.json`. Caught and removed an unspec'd `PATCH /flows/datatables/{id}` route immediately.
- **known_broken ratchet inverted**: if a known-broken example dir now passes idempotency, the smoke harness fails with "congratulations, remove this entry" instead of silently skipping.
- **flow `flowAction` no longer masks DB errors as 404**: branches on `errors.Is(err, models.ErrNotFound)`.
- **flow PUT no longer bypasses the state machine**: `state` + `lockedUser` are stripped from the PUT-merge keys; only `/api/v2/flows/actions/*` can transition.
- **flow PUT multipart with no file part now 400s**: silent no-op was a production-shaped foot-gun.
- **`/routing/queues/{id}/members?delete=true`**: POST with the delete flag now removes the listed users instead of unconditionally adding.
- **`GET /api/v2/users` honors `?state`**: default is `active` (mirrors real Genesys); `state=deleted` filters soft-deleted; `state=any` returns everything.
- **`routing_queue_members.user_id`** now carries a FK to `users(id) ON DELETE CASCADE`.
- **datatable row update/delete** now report "architect_datatable not found" instead of "architect_datatable_row not found" when the parent is missing.
- **Bearer scheme case-insensitive** per RFC 6750 § 2.1: `bearer xyz` now accepted.
- **Test coverage extended** for: full flow state machine (checkin + unlock + PUT-bypass attempt), empty multipart 400, delete-flag POST on members, `?state` user list filtering, FK CASCADE on user → membership.

### Added (S111)
- **5 architect / responsemanagement / IDP resources**: `genesyscloud_architect_datatable` (with rows sub-resource + FK cascade), `genesyscloud_architect_user_prompt` (unique name), `genesyscloud_flow` (multipart upload + lock/publish state machine), `genesyscloud_responsemanagement_response`, `genesyscloud_idp_generic` (singleton).
- **`flow` lock/publish state machine**: `POST /api/v2/flows/actions/{checkout,checkin,publish,unlock,revert,deactivate}?flow={id}`. Transitions persisted in `flows.state`. Initial state `unpublished`; checkout → `locked` + sets `lockedUser`; publish → `published` + clears lock.
- **`flow` multipart upload**: PUT accepts `multipart/form-data` (any file part) OR `application/json` (top-level merge). Multipart file content + filename persisted opaquely in `body.multipartContent` / `body.multipartFilename` for round-trip GET.
- **`/flows/datatables` routes registered BEFORE `/flows/{flowId}`** so chi matches the static `datatables` segment first.
- **`architect_datatable` rows**: CRUD per row with `key` as the row's unique ID. Duplicate key → 409. FK cascade on parent delete.
- **`idp_generic`** singleton: initial GET → 404 (not configured); PUT installs; DELETE removes.
- **Test coverage** (`handlers/architect_test.go`, 7 tests): datatable + rows lifecycle + cascade; user prompt lifecycle + dup-name 409; flow state machine + multipart round-trip; response lifecycle; IDP singleton GET/PUT/DELETE round-trip.
- **15 example dirs** (5 × {working, updates, misconfigured}). `flow/working` ships a real flow YAML; `flow/updates` exercises v1/v2 YAML swap.
- **`/mock/state`** extended for architect (per-table walk + `gatherDatatableRows` for topology + `gatherIDPGeneric` singleton).
- **`coverage_matrix.yaml`** + `docs/spec-notes/architect.md` updated.

### Added (S110)
- **5 routing resources** with full CRUD: `genesyscloud_routing_queue` (POST/GET/list + PUT/DELETE + members sub-resource with idempotent PATCH set-replace + CASCADE delete), `genesyscloud_routing_skill` (POST/GET/list + PATCH/DELETE), `genesyscloud_routing_wrapupcode` (POST/GET/list + PUT/DELETE), `genesyscloud_routing_language` (POST/GET/list + DELETE — no PUT/PATCH per spec), `genesyscloud_routing_utilization` (singleton: GET/PUT/DELETE).
- **FK cascade**: `routing_queue_members` references `routing_queues(id) ON DELETE CASCADE`. Deleting a queue purges memberships.
- **Idempotent member set replace**: `PATCH /api/v2/routing/queues/{queueId}/members` wipes the queue's existing members and re-inserts the request body in a single transaction.
- **Singleton utilization**: `routing_utilization` is keyed by `id='_'`; initial GET returns `{"utilization":{}}` when no PUT has been issued.
- **Test coverage**: lifecycle + dup-name 409 + member idempotency + member 404 on missing queue + cascade behavior + singleton round-trip across all 5 resources (`handlers/routing_test.go`, 8 tests).
- **15 example dirs** (5 routing × {working, updates, misconfigured}). `routing_language` updates dir holds suffix invariant (the spec doesn't allow PUT/PATCH). `routing_utilization` misconfigured dir is a Reverse-Fidelity placeholder noted inline.
- **`/mock/state`** extended for routing: `routing_queue_members` returns the membership grid for topology derivation; `routing_utilization` returns the singleton body or default.
- **`coverage_matrix.yaml`** + `docs/spec-notes/routing.md` updated.

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
