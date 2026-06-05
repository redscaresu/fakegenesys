# fakegenesys Agent Working Agreement

For AI coding agents. Human contributors: see `CONTRIBUTING.md`.

## Mission

fakegenesys is a local Go-based mock of the **Genesys Cloud CCaaS**
HTTP surface, used by [infrafactory](https://github.com/redscaresu/infrafactory)
for deterministic OpenTofu generation validation against the
`mypurecloud/genesyscloud` Terraform provider.

Default port: `:8083` (mockway uses `:8080`, fakegcp `:8081`, fakeaws `:8082`).

## Architecture

Single binary, single process, single chi router, single SQLite handle.

```
cmd/fakegenesys/main.go    flag parsing, server bootstrap
  └─ handlers/
       handlers.go         Application struct, router setup, bearer middleware
       admin.go            /mock/{reset,snapshot,restore,state}
       oauth.go            POST /oauth/token + in-process token store
       <resource>.go       per-resource handlers (S109+)
  └─ models/models.go      ErrNotFound, ErrConflict, ErrBadRequest, PagedResponse[T], ErrorResponse
  └─ repository/repository.go  SQLite handle, Cache interface, lifecycle
  └─ testutil/testutil.go  NewTestServer(t) — fresh per-test instance + Bearer token
  └─ examples/
       provider_smoke_test.go      auto-discovery harness (FAKEGENESYS_ENABLE_E2E=1)
       spec_cross_reference_test.go  every implemented route must exist in specs/
       {working,misconfigured,updates}/<resource>/  per-tree contract
  └─ specs/genesys-openapi.json    primary source of truth for wire shapes
```

## API conventions

- **Auth**: OAuth2 `client_credentials` grant at `POST /oauth/token`.
  Issues a UUID Bearer token with `expires_in=3600`. Validated by
  `bearerAuth` middleware on every route except `/oauth/token`,
  `/mock/*`, and `/healthz`.
- **Pagination**: `pageNumber` + `pageSize` query string. Response:
  `{entities, pageCount, pageNumber, pageSize, total}`.
- **Errors**: `{status, code, message, contextId}` JSON body. 401 on
  missing/invalid Bearer, 404 on FK violation (parent not found), 409
  on dependent-resource delete, 400 on body validation failure.
- **Singleton resources** (`routing/utilization`, `idp/generic`): PUT
  only, no POST, no `id` in the URL.

## Fidelity strategy

**Spec-driven**, mirroring mockway. Source of truth:
`specs/genesys-openapi.json` (Genesys Cloud Swagger 2.0, filtered to
the endpoints fakegenesys implements, ~200KB).

Genesys publishes the raw spec at `https://api.mypurecloud.com/api/v2/docs/swagger`
(currently ~20MB / 2000+ endpoints — the bulk is analytics /
conversations / telephony, all outside fakegenesys's CCaaS-Terraform
scope). `make specs-refresh` re-downloads and filters via jq down to
the identity / routing / architect / responsemanagement / IDP
endpoints, dropping the per-operation schemas so the committed
artifact is small enough to diff in PR review.

**Per-slice contract**:

1. Before writing a handler, locate the resource's endpoints in the
   spec. Capture request/response shapes + pagination contract + any
   documented error codes into `docs/spec-notes/<resource>.md`.
   Required reading before the handler tickets.
2. Build the handler against the spec, not against guessed shapes.
3. **Reverse fidelity — don't over-correct**: never enforce
   constraints (required fields, format validators, cascade blockers)
   the OpenAPI spec doesn't declare. If unsure, omit the validation —
   the real provider will reject genuinely-bad configs before they
   reach fakegenesys. (Borrowed verbatim from
   `../mockway/AGENTS.md` § "Anti-patterns".)
4. Add the new route(s) to the `ImplementedRoutes` list at the top of
   the handler file's package init OR have
   `examples/spec_cross_reference_test.go` discover them via the
   route map — the test asserts every implemented route exists in the
   spec.
5. The provider smoke harness (`examples/provider_smoke_test.go`) is
   the second line of defense. Spec cross-reference catches typos;
   smoke harness catches behavioral divergence.

## Provider smoke harness

Every resource ships three example directories:

- `examples/working/<resource>/main.tf` + `providers.tf` — exercises CRUD.
- `examples/updates/<resource>/main.tf` + `v1.tfvars` + `v2.tfvars` —
  pins idempotent updates (change a mutable attribute, plan-no-op
  after re-apply).
- `examples/misconfigured/<resource>/main.tf` + optional `expected.txt` —
  exercises a documented error path.

The harness is gated by `FAKEGENESYS_ENABLE_E2E=1`. It builds the
fakegenesys binary once, then spawns a fresh per-test instance on a
random local port. `localhost:8083` / `127.0.0.1:8083` in
`providers.tf` are rewritten to the per-test port automatically. Each
example dir is its own `t.Run` sub-test.

`examples/known_broken.yaml` is the ratchet allowlist for
idempotency-only failures (apply + destroy still run, but
`plan -detailed-exitcode` is skipped). Empty initially. Adding an
entry requires a tracking ticket; removing one requires confirming
the dir now passes idempotency clean.

## Per-bundle PR rule

When adding a new resource (S109+), the SAME PR must include:

1. **Spec note**: `docs/spec-notes/<resource>.md` documenting endpoints
   + request/response shapes + pagination + error codes.
2. **Handler**: `handlers/<resource>.go` (CRUD + list + any state-machine
   transitions, e.g., `flow` publish).
3. **Handler tests**: `handlers/<resource>_test.go` (lifecycle + FK
   rejection + pagination boundary + idempotent create-then-update).
4. **Examples**: `working/<resource>/`, `updates/<resource>/`,
   `misconfigured/<resource>/` (where applicable — singletons skip
   updates).
5. **State export**: extend `collectState` in `admin.go` so
   `/mock/state` returns the new resource type.
6. **Coverage matrix**: append `<resource>` to `coverage_matrix.yaml`
   (added when the first non-OAuth resource lands).

## Testing

```bash
make test                # all packages, no E2E
make test-race           # race detector
make test-coverage       # handlers/... coverage report

FAKEGENESYS_ENABLE_E2E=1 go test ./examples/... -timeout 30m
```

`testutil.NewTestServer(t)` returns a fresh per-test fakegenesys + a
pre-minted Bearer token. Helper methods (`PostJSON`, `GetJSON`,
`PutJSON`, `PatchJSON`, `DeleteJSON`) auto-set the Authorization
header.

## Admin endpoints

All unauthenticated (mirror of mockway / fakegcp / fakeaws):

| Endpoint | Method | Purpose |
|---|---|---|
| `/healthz` | GET | liveness — used by smoke harness boot wait |
| `/mock/reset` | POST | wipe SQLite tables + reset all caches (including token store) |
| `/mock/snapshot` | POST | `VACUUM INTO <dbPath>.snapshot` (409 on `:memory:`) |
| `/mock/restore` | POST | swap snapshot back in (404 if no snapshot, 409 on `:memory:`) |
| `/mock/state` | GET | full topology — schema_version=1, per-resource arrays |
| `/mock/state/{service}` | GET | single-resource topology |

## Anti-patterns

- **No silent 200s.** Unimplemented endpoints return 501 with a log
  line. The next caller sees what's missing.
- **No moto-style fallback.** chi `r.NotFound` and `r.MethodNotAllowed`
  both return 501.
- **No fault injection.** This is a fast-feedback inner loop; latency /
  fault simulation defeats the purpose. (Killed pattern carried over
  from fakeaws S49.)
- **No hand-edited pitfalls.** When infrafactory's sweep surfaces a
  mock-server-bug-classified failure, the fix lands HERE in fakegenesys,
  not in infrafactory's `pitfalls/genesys.yaml`. Mirrors
  `feedback_sweep_protocol.md`.

## Safe workflow

```bash
git status --short
git branch --show-current
go test ./...
```

If `go test` is red on a clean checkout, restore to green before
starting a new slice.

When touching a handler, run the affected tree of the smoke harness
locally before pushing:

```bash
FAKEGENESYS_ENABLE_E2E=1 go test ./examples/... -run TestProviderSmokeWorking -v
```

## Secrets

Same protections as the other three siblings. `.gitleaks.toml` +
pre-commit hook block accidental commits. `examples/*.tf` is
allowlisted for placeholder credentials only.
