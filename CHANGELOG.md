# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.1] - 2026-06-11

The example-drift fix arc (S136–S138). Closes the gap where the
standalone smoke test (`FAKEGENESYS_ENABLE_E2E=1 go test ./examples/...`)
failed for two distinct reasons from a fresh clone — environment
plumbing that the infrafactory harness provided automatically but
`go test` did not, and HCL drift in several examples against the
current `mypurecloud/genesyscloud` provider schema.

### Added (S136)
- **`examples/provider_smoke_test.go::setupGenesysProviderEnv`** —
  the smoke harness now wires the genesyscloud provider's full
  environment automatically per sub-test:
  `GENESYSCLOUD_OAUTHCLIENT_ID/SECRET/REGION`, `HTTPS_PROXY` +
  `HTTP_PROXY` routing through the TLS MITM (api_port + 360),
  `NO_PROXY` for external registries, `SSL_CERT_FILE` pointing at
  a CA fetched from `/mock/ca-cert` and written to `t.TempDir()`,
  and `FAKEGENESYS_UPLOAD_HOST` so the flow upload-job handler
  embeds the per-test listener address in its presignedUrl.

### Fixed (S137)
- **`handlers/flow.go::handleFlowJobCreate`** — respects
  `FAKEGENESYS_UPLOAD_HOST` env var as the highest-priority source
  for the upload URL's host:port. Falls back to `r.Host` (httptest
  path) then `localhost:8083` (production default). Previously,
  when reached through the MITM proxy, `r.Host` reflected the
  upstream Genesys domain rather than the local listener; the
  provider's follow-up PUT to the upload URL would route back
  through HTTP_PROXY and get 405 from the CONNECT proxy.

### Fixed (S137 + S138) — example HCL drift
8 examples in `examples/working/` and 8 in `examples/updates/` plus
1 misconfigured/ refreshed against the current provider schema:

- `oauth_client.authorized_grant_type`: `CLIENT_CREDENTIALS` →
  `CLIENT-CREDENTIALS` (hyphen, not underscore)
- `idp_generic.certificate` (singular) → `certificates` (list)
- `flow.file_content_hash` dropped (now provider-computed)
- `architect_datatable.schema = jsonencode({...})` → `properties { name, type }` blocks
- `location` now declares `address {...}` + `emergency_number {...}` with required subfields (`street1`, `city`, `zip_code`, `country`, `number`)
- `responsemanagement_response` now requires a parent
  `genesyscloud_responsemanagement_library` referenced via `library_ids`
- `routing_utilization.utilization { media_type }` block → named per-media-type blocks (`call {}`, `email {}`, etc.)
- `routing_skill.description` removed (provider deprecated the field)
- `misconfigured/oauth_client/expected.txt` updated to match the provider's current error wording (`authorized_grant_type`)

### Verified
- `FAKEGENESYS_ENABLE_E2E=1 go test ./examples/...` runs 100% green
  in ~205s from a fresh clone with zero manual env setup
- `known_broken.yaml` is empty

## [0.2.0] - 2026-06-10

The v0.2 hardening arc — closes the standalone-quality gaps fakegenesys
had vs siblings at v0.1.0, locks in a CI-enforced contract-coverage
convention shared across the family.

### Added (S123)
- **17 `TestContract_*` regression tests** locking in every wire-shape
  invariant the `mypurecloud/genesyscloud` Terraform provider depends
  on across the post-S116/S122 handler surface. See
  `docs/contract-matrix-s123.md` for the per-row matrix.
- **`CRITICAL[<id>]:` / `MUST[<id>]:` docstring convention** in each
  handler — the matrix's source of truth for what each test locks in.
  Two tests (`TestContract_tokens_me_oauthclient_pascal_case` and
  `TestContract_users_search_results_key`) inspect raw response bytes
  because Go's `json.Unmarshal` is case-insensitive and would
  otherwise mask the regression.

### Added (S125)
- **`.github/workflows/docker.yml`** — multi-arch (linux/amd64 +
  linux/arm64) container build pushing to
  `ghcr.io/redscaresu/fakegenesys:{latest, <sha>}`. Triggers on the
  `ci` workflow completing successfully on main (NOT a nightly
  schedule). Sibling-parity with mockway, fakegcp, fakeaws.

### Added (S124)
- **`docs/review-passes/pass3.md`** documents pass 3 + pass 4, both
  returning `NOTHING_TO_IMPROVE` against the post-S116/S122 surface.
  Review loop closes per the anti-nitpick rule (two consecutive
  no-substantive passes).

### Added (S126+S127)
- **`handlers/contract_audit_test.go`** — durable CI-enforced check
  that every `CRITICAL[<id>]:`/`MUST[<id>]:` docstring has a paired
  `TestContract_<id>` and vice versa. Drift becomes a failed
  `go test`, not a missed code review. Includes a self-test
  (`TestContractAuditTest_Self`) so the audit itself stays correct.
  Empty-contracts-safe — sibling fakes adopt the file before sweeping
  existing notes.
- **README "Testing examples" stanza** — canonical entry point is
  `go test ./examples/...` (gated by `FAKEGENESYS_ENABLE_E2E=1`).
  Same wording landed across mockway, fakegcp, fakeaws.

### Fixed (S123)
- **`handlers/flow.go::handleFlowJobCreate`**: derive `uploadHost`
  from `r.Host` (with `localhost:8083` fallback) instead of
  hardcoding the production port. In tests `httptest` binds a random
  port; the previous hardcoded URL would 503 outside CI environments
  that happened to have fakegenesys already on `:8083`.

## [0.1.0] - 2026-06-10

The initial fakegenesys release — Genesys Cloud CCaaS mock for the
infrafactory generate→validate loop. 44/44 deterministic sustain
sweep at full scope. Tagged at commit `ba2de5a` (S122g).

### Added (S116c, S119, S122/a/b/c/d/f/g)
- **Post-auth SDK probe endpoints**: `/organizations/me`,
  `/authorization/products` (with non-nil `total`),
  `/authorization/divisions{,/home}`, `/tokens/me` (with PascalCase
  `OAuthClient.organization.id == "purecloud-builtin"`).
- **OAuth `client_credentials` Basic Auth** per RFC 6749 § 2.3.1.
- **`POST /users/search`** returning `{results:[...]}` (paged-list
  key is `results`, NOT `entities` — provider reads
  `Usersearchresponse.Results`).
- **User subresources** the SDK reads/writes on user CRUD:
  `/users/{id}/routingskills`, `/routinglanguages`, `/roles` GET+PUT,
  `/password` POST → 204.
- **Default Home division** on `users` + `architect_datatables` create
  (no `division` in body still round-trips a non-empty `division.id`).
- **Routing queue create returns HTTP 200** (NOT 201, provider gates
  on 200) + **`memberCount` derived at GET time** from the
  `routing_queue_members` table.
- **Routing queue wrapup-code associations**:
  `/routing/queues/{id}/wrapupcodes` GET/POST/DELETE.
- **Voicemail userpolicy + routing utilization** stubs.
- **Group subresources**: `/groups/{id}/individuals` (membership list
  the provider polls), `/groups/{id}/members` POST/DELETE (associate +
  bulk remove via `?id=u1,u2`), `/groups/{id}/voicemail` (legacy) AND
  `/voicemail/groups/{id}/policy` (modern) for GET + PATCH.
- **`/users/me`** and **`/users/{id}/roles`** GET/PUT for the
  terraform-user role chain after the oauth_client crash fix.
- **`/authorization/subjects/{id}`** GET + bulkadd/bulkremove POSTs
  (`grants` array must be non-nil — provider iterates it).
- **`/responsemanagement/libraries`** CRUD (parent container for
  `responsemanagement_response`).
- **Flow upload-job protocol**: `POST /flows/jobs` returns
  `presignedUrl` + `id`; PUT to that URL accepts the YAML; GET
  `/flows/jobs/{jobId}` polled until `status: "Success"` + non-empty
  `flow.id`. Auto-derives upload URL from `r.Host` (S123 fix).
- **`architect_datatables`**: round-trip a non-empty `division.id` so
  the provider's `*datatable.Division.Id` deref doesn't segfault.

### Added (S116)
- **TLS MITM CONNECT proxy** on `:8443` (`--tls-port`; set `0` to disable). The `mypurecloud/genesyscloud` Terraform provider ignores `GENESYSCLOUD_GATEWAY_*` env vars and hardcodes `login.<region>.pure.cloud`. The proxy makes `HTTPS_PROXY=http://localhost:8443` route every provider call (auth + API) through fakegenesys without modifying the provider.
- **Boot-time CA** generated in `NewApplication` (self-signed, 10-yr, 2048-bit RSA). Leaf certs dynamically issued per-hostname (SAN'd for the requested host) and cached for process lifetime.
- **`GET /mock/ca-cert`** returns the PEM CA so harnesses can write it to `SSL_CERT_FILE` and trust the MITM chain at runtime.
- **Tests** (`handlers/tls_mitm_test.go`): CA endpoint shape; full HTTPS_PROXY round-trip (`POST https://api.mypurecloud.com/login/oauth/token` then `GET https://api.mypurecloud.com/api/v2/users`); non-CONNECT method returns 405.
- AGENTS.md § "TLS MITM proxy" documents the wire flow + ports + cert lifecycle.

### Changed / Fixed (S113)
- **Restore tmpPath leak on copyFile failure**: all error paths in `repository.Restore` now `os.Remove(tmpPath)` to avoid leaving stale staging files on disk.
- **Restore corruption on Rename failure**: snapshot the pre-Restore bytes into memory BEFORE closing the existing handle; on rename failure, write the snapshot back and reopen so the repo rolls back to the pre-Restore state instead of leaving a dead `r.db`.
- **`requireQueueExists` / `requireDatatableExists` / datatable row update existence check** now branch on `sql.ErrNoRows`: real DB errors no longer masquerade as 404. (Same fix pass 1 applied to `flow.flowAction`; pass 2 caught the 3 sibling locations.)
- **Callers updated** (7 call sites across `routing_queue.go` + `architect_datatable.go`) to differentiate ErrNotFound → 404 from other errors → 500.
- **Pass-3 sanity check folded in-slice** — no new substantive findings; review loop closed per the anti-nitpick rule (two consecutive no-substantive passes).

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
