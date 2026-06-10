# Code review pass 3 (S124)

Pass 3 reviewed the post-S116/S122 surface that landed in S123 (16-file
contract-test backfill PR #22, commit 33742ca + flow.go r.Host fix
e7d1354) plus the S125 docker.yml (PR #23). This is the first review
after the codex loop reopened post-S113.

**Result**: `NOTHING_TO_IMPROVE`. 0 substantive findings.

## Scope

- 8 handler files with new `CRITICAL[<id>]:` / `MUST[<id>]:` notes
  (organization.go, user.go, user_subresources.go, architect_datatable.go,
  flow.go, group.go, responsemanagement.go, routing_queue.go, oauth.go)
- 5 test files with new `TestContract_<id>` functions across 17 contracts
  (organization_test.go, identity_test.go, architect_test.go,
  routing_test.go, oauth_test.go)
- `docs/contract-matrix-s123.md` row-vs-source consistency
- `.github/workflows/docker.yml` sibling-parity check
- `handlers/contract_audit_test.go` (S127 reference impl on a separate
  branch; reviewed inline so S127 PR doesn't need its own review pass)

## What was specifically validated

- **Raw-byte inspection where required**:
  `TestContract_tokens_me_oauthclient_pascal_case` uses `bytes.Contains`
  before `json.Unmarshal` — Go's case-insensitive unmarshal would
  otherwise mask the PascalCase regression. Same defense in
  `TestContract_users_search_results_key` (raw `results` vs `entities`).
- **`r.Host` derivation in `flow.go`**: `handleFlowJobCreate` derives
  `uploadHost` from `r.Host` with `localhost:8083` fallback — no
  hardcoded port. Caught and fixed mid-S123 PR after CI flagged it.
  `TestContract_flow_jobs_upload_protocol` asserts the presignedUrl
  points at `localhost` or `127.0.0.1` (NO_PROXY invariant).
- **`docker.yml`**: `workflow_run` on the lowercase `ci` workflow
  (matches `ci.yml`'s `name: ci`), branch-filtered to main, gated on
  `conclusion == 'success'`. NOT a nightly schedule. Identical step
  shape to mockway and fakegcp's docker.yml.
- **204 contracts** (`user-password`, `subjects-bulkadd-bulkremove`):
  handlers `WriteHeader(StatusNoContent)` unconditionally; tests assert
  `status == 204` AND empty body.
- **Default-division round-trip** tests assert non-empty `division.id`
  on BOTH the create response AND the read-after-create. Correct
  coverage — provider's deref happens on read.
- **`memberCount` derivation** asserted via the GET-time COUNT(*)
  invariant, matching the handler's docstring claim.
- **Group voicemail dual-paths**: both legacy `/groups/{id}/voicemail`
  AND modern `/voicemail/groups/{id}/policy` registered + tested for
  GET + PATCH, with sub-test per path so regressions surface
  independently.
- **Contract audit self-test**: known-good, known-bad-missing-test,
  known-bad-orphan-test fixtures + kebab/snake round-trip. The
  audit's own file is excluded from scans so fixture strings don't
  pollute. Empty-state passes (sibling-adoption safe).

## Declined nitpicks

None. Pass 3 found nothing worth flagging — substantive or otherwise.

# Code review pass 4 (S124)

Pass 4 was a confirmation pass — fresh-eyes review of the same surface
explicitly instructed NOT to defer to pass 3.

**Result**: `NOTHING_TO_IMPROVE`. 0 substantive findings.

## What pass 4 also specifically looked for

- Any false-positive test (would pass even if the invariant were
  broken). None found.
- Audit regex edge cases. Noted that `(?:CRITICAL|MUST)\[([a-z0-9][a-z0-9-]*)\]`
  silently ignores uppercase IDs — but this is theoretical drift
  rather than current exposure since the established convention is
  kebab-lowercase. Documented in `feedback_oss_mature_day_one.md`
  item 14.
- docker.yml `permissions: packages: write` without `contents: read`
  — matches the sibling pattern exactly; works for public repos.

## Loop closure

Two consecutive passes returned `NOTHING_TO_IMPROVE`. Per
`feedback_codex_anti_nitpick.md` the review loop CLOSES here. Future
arcs reopen with a fresh pass against the new surface.
