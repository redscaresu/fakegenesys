# Code review pass 1 (S112)

Generated 2026-06-06 by an automated review pass over the fakegenesys
codebase after S108+S109+S110+S111 landed. 13 substantive findings
surfaced; 11 fixed in this slice (S112), 2 deferred with rationale
below. ~13 declined nitpicks also recorded for future-reviewer context.

## Substantive findings — fixed

### 1. `repository.Restore` corruption on failure
**Where**: `repository/repository.go::Restore`.
**Bug**: closed the existing DB handle BEFORE attempting the copy + reopen. If either failed, `r.db` remained the closed handle and every subsequent call returned "database is closed".
**Fix**: stage the new DB at `<dbPath>.restore-tmp`, open + verify, only then close the existing handle and rename into place. On any error path, the old handle stays usable.

### 2. `spec_cross_reference_test` was a silent no-op
**Where**: `examples/spec_cross_reference_test.go`.
**Bug**: relied on an `ImplementedRoutes` slice that nothing populated. The test passed by logging "ImplementedRoutes empty" even after 15 resources landed.
**Fix**: walk the live chi route tree via `chi.Walk(app.Router(), ...)` and assert every `/api/v2/...` route exists in `specs/genesys-openapi.json`. Caught one bug immediately: `PATCH /flows/datatables/{id}` was registered but absent from the spec — removed the PATCH route per Reverse Fidelity.

### 3. `known_broken` ratchet didn't ratchet
**Where**: `examples/provider_smoke_test.go::runWorkingExample`.
**Bug**: broken dirs simply skipped the plan-no-op assertion. If a broken dir started passing idempotency, nothing reported it.
**Fix**: for broken dirs, run `tofuPlanIsNoOp` and fail with "congratulations, remove this entry" if it returns true. Empty `known_broken.yaml` today, but the gate is real now.

### 4. `flowAction` masked all DB errors as 404
**Where**: `handlers/flow.go::flowAction`.
**Bug**: any `scanOneJSON` error → 404. The other handlers branch on `errors.Is(err, models.ErrNotFound)`.
**Fix**: branch correctly. Non-NotFound errors → 500.

### 5. POST `/routing/queues/{id}/members?delete=true` ignored the query
**Where**: `handlers/routing_queue.go::handleRoutingQueueMembersAdd`.
**Bug**: per spec the `delete=true|false` query controls add vs remove. fakegenesys unconditionally INSERT OR REPLACE'd.
**Fix**: read `?delete` and branch to the DELETE-by-id path. Test pinned.

### 7. `GET /api/v2/users` ignored the `state` query
**Where**: `handlers/user.go::handleUserList`.
**Bug**: real Genesys returns only `active` users by default; `state=deleted` filters to soft-deleted; `state=any` returns everything. fakegenesys returned ALL rows regardless. Terraform provider's user data source would see ghost rows after a destroy + re-apply.
**Fix**: filter by state with `?state=active` as the default.

### 8. `routing_queue_members.user_id` had no FK to `users(id)`
**Where**: `repository/repository.go::migrate`.
**Bug**: orphaned `user_id` values stayed in the membership table forever.
**Fix**: add `FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE`. Test added (with a note that fakegenesys's soft-delete on users doesn't trigger CASCADE since the row stays — divergence documented in the test).

### 9. `flow` state machine bypassable via PUT
**Where**: `handlers/flow.go::handleFlowUpdate`.
**Bug**: PUT's body-merge included `state` and `lockedUser`, so a caller could `PUT {state:"published"}` and skip the `/actions/*` state machine entirely.
**Fix**: strip `state` + `lockedUser` from the PUT-merge keys. Test pinned with explicit bypass-attempt assertion.

### 10. `TestFlow_LifecycleAndStateMachine` covered only 2 of 4 transitions
**Where**: `handlers/architect_test.go::TestFlow_LifecycleAndStateMachine`.
**Bug**: tested only checkout → publish. Missed `checkin` (locked → unpublished) and `unlock` (forced unlock).
**Fix**: extended the test to cover all 4 transitions plus the PUT-bypass attempt from finding #9.

### 11. Multipart `flow` PUT with no file part silently no-op'd
**Where**: `handlers/flow.go::handleFlowUpdate`.
**Bug**: multipart with `Content-Type: multipart/form-data` but no file parts ran the empty loop and returned 200 OK without touching the flow body. Provider could silently submit nothing and get success.
**Fix**: track whether a file part was found; return 400 "multipart upload missing file part" if not. Test pinned.

### 12. Datatable row sub-handlers misclassified "parent missing" as "row missing"
**Where**: `handlers/architect_datatable.go::handleDatatableRowUpdate` + `handleDatatableRowDelete`.
**Bug**: skipped `requireDatatableExists`, so a request for a row of a non-existent datatable returned "architect_datatable_row not found" (instead of "architect_datatable not found").
**Fix**: call `requireDatatableExists` first in both handlers.

### 13. Bearer scheme was case-sensitive
**Where**: `handlers/handlers.go::bearerAuth`.
**Bug**: `strings.HasPrefix(auth, "Bearer ")` rejected lowercase `bearer xyz`. RFC 6750 § 2.1 requires case-insensitive matching on the scheme.
**Fix**: `strings.EqualFold` on the 7-char prefix.

## Substantive findings — deferred with rationale

### 6. PATCH `/routing/queues/{id}/members` "set-replace vs per-row toggle"
**Where**: `handlers/routing_queue.go::handleRoutingQueueMembersReplace`.
**Finding**: spec wording "Join or unjoin a set of up to 100 users" is ambiguous between full-set replace (current implementation) and per-row joined-flag toggle.
**Deferred because**: the current set-replace semantics match the Terraform provider's usage pattern for HCL-managed sets (one resource, replaces its declared members on every apply). Changing to per-row would break the existing test and require new behavior the test doesn't exercise. Re-evaluate when a scenario surfaces a case where per-row toggle is observable.

## Declined nitpicks

Logged for future-reviewer context:

1. `var _ chi.Router` / `var _ = sql.ErrConnDone` import-anchors — load-bearing during incremental edits.
2. `iToStr` (identity_test.go) only supports 0-99 — test range is 0-30; not a real bug.
3. `gatherIDPGeneric` returns `{}` for unconfigured while the API returns 404 — intentional: two audiences (topology vs user-facing), two shapes.
4. `freePort` is theoretically racy — standard Go test idiom.
5. `http.ListenAndServe` lacks Slowloris timeouts — local-only mock, not a real concern.
6. `cmd.Process.Kill()` vs SIGTERM-then-Kill — tests don't need graceful shutdown.
7. `architect_user_prompt` per-language `resources` sub-resource not modelled — Reverse Fidelity; adds when a scenario drives it.
8. `auth_role` permissions catalog not validated — Reverse Fidelity, documented anti-pattern.
9. `routing_queue_members.ringNumber` defaults to 1 when 0 — matches real Genesys behavior.
10. `oauth_clients/{id}/secret` re-mint endpoint not implemented — adds when a scenario requires it.
11. `architect_datatable.schema` not enforced against rows — Reverse Fidelity, documented in file header.
12. Missing `rows.Err()` after iterator drains in some handlers — extreme edge case for SQLite single-conn driver.
13. `tx.Rollback()` after `tx.Commit()` returns `sql.ErrTxDone` — standard Go idiom; intentionally swallowed.

## Net outcome of pass 1

| Category | Count |
|---|---|
| Substantive findings fixed | 11 |
| Substantive findings deferred (with rationale) | 1 (#6) |
| Nitpicks declined | 13 |

Tests green: `go test ./...` and `go test -race ./...`.

## Files touched by S112

- `repository/repository.go` (Restore staging + members FK)
- `handlers/handlers.go` (Bearer case)
- `handlers/flow.go` (4 fixes: error mask, PUT bypass, empty multipart, state machine)
- `handlers/routing_queue.go` (delete=true)
- `handlers/user.go` (state filter)
- `handlers/architect_datatable.go` (PATCH removal + 404 attribution)
- `examples/spec_cross_reference_test.go` (full rewrite — chi.Walk)
- `examples/provider_smoke_test.go` (ratchet inversion)
- `handlers/architect_test.go` (extended state-machine + empty-multipart tests)
- `handlers/routing_test.go` (delete=true + state filter tests)
