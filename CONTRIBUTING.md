# Contributing to fakegenesys

`fakegenesys` is the Genesys Cloud CCaaS mock for the [InfraFactory](https://github.com/redscaresu/infrafactory) project. It simulates the `mypurecloud/genesyscloud` Terraform provider's HTTP API surface against a local SQLite database so InfraFactory's Layer 2 validation can run offline against a deterministic backend.

## TL;DR

1. Open an issue first for non-trivial changes (especially new resources).
2. Each resource lands as a **bundle**: spec note + handler + tests + examples (3 trees) + `coverage_matrix.yaml` entry + `/mock/state` extension — all in one PR.
3. `make test` must be green.
4. Pre-commit hook (`make install-hooks`) runs `gitleaks` + `go test`.
5. Spec-driven fidelity: consult `specs/genesys-openapi.json` BEFORE writing a handler.

## Setup

Required: Go 1.25+, `make`. Optional: `gitleaks` + `tofu` (for the smoke harness).

```bash
git clone https://github.com/redscaresu/fakegenesys.git
cd fakegenesys
go mod download
make install-hooks
make test
make run    # serves the mock at :8083
```

## Per-bundle rule

When you add a new Genesys resource to fakegenesys, the SAME PR must include:

1. **Spec note** (`docs/spec-notes/<resource>.md`): document the
   endpoints + request/response shapes + pagination + error codes
   you extracted from `specs/genesys-openapi.json`. Required reading
   before the handler implementation.
2. **Handler** (`handlers/<resource>.go`): CRUD + list + any
   state-machine transitions (e.g., `flow` publish).
3. **Handler tests** (`handlers/<resource>_test.go`): lifecycle
   (Create → Get → List → Delete → 404), FK rejection, pagination
   boundary, idempotent create-then-update.
4. **Examples**: `examples/working/<resource>/`,
   `examples/updates/<resource>/`,
   `examples/misconfigured/<resource>/` (singletons skip updates).
5. **State export**: extend `collectState` in `handlers/admin.go` so
   `/mock/state` returns the new resource type.
6. **`coverage_matrix.yaml`** entry.

## Fidelity strategy

fakegenesys is **spec-driven**, mirroring [mockway](https://github.com/redscaresu/mockway). The committed `specs/genesys-openapi.json` is the primary source of truth.

**Reverse fidelity — don't over-correct**: never enforce constraints (required fields, format validators, cascade blockers) the OpenAPI spec doesn't declare. If unsure, omit the validation — the real Terraform provider will reject genuinely-bad configs before they reach fakegenesys.

## Code style

- Standard `gofmt` (the pre-commit hook enforces this).
- Errors use the sentinel pattern (`models.ErrNotFound`, etc.) — handlers translate to HTTP status via `errors.Is`.
- No magic numbers; constants live at the top of the handler file.
- Tests use `testutil.NewTestServer(t)` — fresh per-test fakegenesys, pre-minted Bearer token.

## Anti-nitpick rule

If you run codex review against your PR, triage findings:
- ✅ Act on: wire-shape correctness, missing test coverage that hides a behavioral assumption, broken auth/security, missing 404 fidelity, FK integrity, idempotency violations.
- ❌ Ignore: "could be more idiomatic", "consider renaming X to Y", repeat findings on patterns the other 3 siblings already use.

Stop iterating when two consecutive passes return only nitpicks. Document declined findings with rationale in `docs/review-passes/passN.md`.
