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

## TLS MITM proxy (S116, for HTTPS_PROXY-based provider redirection)

The `mypurecloud/genesyscloud` Terraform provider's auth path ignores
`GENESYSCLOUD_GATEWAY_*` env vars and hardcodes `login.<region>.pure.cloud`.
To make the provider hit fakegenesys without modifying it, the binary
runs a CONNECT-proxy on a second listener (default `:8443`) that
MITM-terminates client TLS using leaf certs dynamically signed by a
boot-time self-signed CA. Decrypted HTTP traffic flows through the same
chi router the plain `:8083` listener uses.

Boot flow:
1. `NewApplication` generates a fresh CA in-memory (10-yr validity, 2048-bit RSA).
2. `cmd/fakegenesys/main.go` starts the proxy goroutine on `:8443` (overridable via `--tls-port`; `--tls-port=0` disables).
3. Harness clients fetch the PEM-encoded CA via `GET /mock/ca-cert` and write it to `SSL_CERT_FILE` so Go's TLS stack trusts the leaf chain.
4. Client sets `HTTPS_PROXY=http://localhost:8443`. The proxy handles `CONNECT api.mypurecloud.com:443`, MITM-handshakes with a leaf cert SAN'd for that hostname, then serves the decrypted request through the chi router. No upstream forwarding — we ARE the upstream.

Leaf certs are cached by hostname for process lifetime. The CA, leaf
generator, and oneConnListener bridge live in `handlers/tls_mitm.go`;
the `/mock/ca-cert` endpoint lives in `handlers/admin.go`. Tests in
`handlers/tls_mitm_test.go` cover CA endpoint shape + the full
HTTPS_PROXY round-trip (token mint then API call against
`https://api.mypurecloud.com`).

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

## Contract-coverage convention (canonical — fakegenesys is the reference impl)

`handlers/contract_audit_test.go` enforces the `CRITICAL[<id>]:` /
`MUST[<id>]:` docstring → `TestContract_<id>` test pairing across
`handlers/*.go`. A wire-shape invariant the consuming
`mypurecloud/genesyscloud` provider depends on must NOT live as a
comment alone — drift becomes a failed `go test`, not a missed code
review.

The convention was born here in S123 (17 contracts paired with the
post-S116/S122 mock-gap surface; see `docs/contract-matrix-s123.md`).
S127 then rolled the same audit out across mockway/fakegcp/fakeaws
as empty-state, and S128–S130 bridged each sibling's existing
wire-shape invariants into the convention (27 paired contracts total
across the family).

Adding a new contract:

1. Add `// CRITICAL[<kebab-case-id>]: <invariant + why it matters>`
   above the handler (or `MUST[<id>]:` inside a code path).
2. Add `func TestContract_<id_with_underscores>(t *testing.T)` to a
   test file in this package. The test must assert the invariant
   (revert the fix → test fails).
3. Append a row to `docs/contract-matrix-s123.md`.

The same file ships in mockway, fakegcp, and fakeaws with the same
regex + paired-test logic. Cross-reference: `feedback_oss_mature_day_one.md`
item 14 (infrafactory memory).

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
