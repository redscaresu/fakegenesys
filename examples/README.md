# examples

Three trees, auto-discovered by `provider_smoke_test.go`:

| Tree | Contract |
|---|---|
| `working/<resource>/` | `tofu init && tofu apply && tofu plan -detailed-exitcode (no diff) && tofu destroy` |
| `misconfigured/<resource>/` | `tofu apply` MUST fail. If `expected.txt` is present, the failure output MUST contain that string fragment. |
| `updates/<resource>/` | apply v1.tfvars → plan no-op → apply v2.tfvars → plan no-op → destroy. Each dir MUST contain `main.tf`, `v1.tfvars`, `v2.tfvars`. |

Per-resource example bundles ship with their handler (S109+). The
smoke harness is gated by `FAKEGENESYS_ENABLE_E2E=1`.

The harness builds the `fakegenesys` binary once, then spawns a fresh
per-test instance on a random local port (no shared mock state across
example dirs). `localhost:8083` and `127.0.0.1:8083` in
`providers.tf` are rewritten to the per-test port automatically.

## known_broken.yaml

Ratchet allowlist for idempotency-only failures. See the file header
for the schema. Entries skip the drift assertion but still exercise
apply + destroy. Empty in S108.
