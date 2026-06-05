# Security Policy

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.
Instead, report privately via:

- GitHub's [private vulnerability reporting](https://github.com/redscaresu/fakegenesys/security/advisories/new) (preferred).
- Email: `ukashouri@gmail.com` with subject prefix `[security] fakegenesys:`.

Include: description, impact, steps to reproduce, affected commit, any mitigations you've identified.

## What to expect

- Acknowledgement within 5 working days.
- Assessment within 14 days of acknowledgement.
- Coordinated disclosure with credit unless you decline.

## Scope

In scope:
- This repository (`redscaresu/fakegenesys`).
- Vulnerabilities in the SQLite-backed mock that would let a crafted HTTP request execute arbitrary code, exfiltrate filesystem contents outside the working directory, or otherwise escape the intended mock surface.
- OAuth token handling — token forgery, leakage via response/log shaping, or bypasses of the bearer middleware.

Out of scope:
- Issues in dependencies (`go-chi`, `modernc.org/sqlite`, etc.) — please report upstream.
- "fakegenesys accepts an invalid Genesys request that real Genesys would reject" — that's a *fidelity gap*, not a vulnerability; file a regular issue with the `fidelity` label.
- "fakegenesys accepts any client_id/client_secret pair on `/oauth/token`" — this is *by design* (see AGENTS.md § "API conventions"). The mock's purpose is wire-shape fidelity, not credential validation.

## Pre-commit hook

`make install-hooks` configures `gitleaks protect --staged` to block accidental credential commits. The hook also runs `go test ./...`. Full-history `gitleaks detect` is run periodically.
