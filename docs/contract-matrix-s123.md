# S123 Contract Matrix

Source-of-truth artifact for the S123 contract-driven test backfill. Every
`CRITICAL[<id>]:` / `MUST[<id>]:` note in `handlers/*.go` has a paired
`TestContract_<id>` (CI-enforced via the audit added in S127). Adding a
row here means adding both the docstring tag AND the test.

Conventions (S127 forward-compatibility):
- Docstring form: `// CRITICAL[<contract-id>]: <existing explanation>` (kebab-case id, descriptive, stable).
- Test form: `TestContract_<contract_id_with_underscores>` (`-` → `_`).
- One-to-one. Each docstring note has exactly one paired test; each test has exactly one paired note.

Three coverage bars:
1. **Regression-per-mock-gap**: each S116/S122 layer that drained a real provider crash or hang has one test.
2. **Docstring-derived**: each `CRITICAL`/`MUST` invariant becomes one assertion.
3. **Nil-deref defenses**: each SDK pointer field the consuming provider dereferences without nil-check gets an explicit non-nil assertion.

## Matrix

| contract_id | source handler | test file | description | sources |
|---|---|---|---|---|
| `authorization-products-total-int` | `organization.go::handleAuthorizationProducts` | `handlers/organization_test.go` | GET `/api/v2/authorization/products` body must include a non-nil integer `total` field matching `len(entities)`. Without it the provider segfaults at `make([]string, *productEntities.Total)` (provider.go:224). | CRITICAL, nil-deref, S116c |
| `tokens-me-oauthclient-pascal-case` | `organization.go::handleTokensMe` | `handlers/organization_test.go` | GET `/api/v2/tokens/me` body must use the PascalCase JSON key `OAuthClient` (not camelCase) with `OAuthClient.organization.id == "purecloud-builtin"`. SDK's custom UnmarshalJSON does case-sensitive map lookup; case mismatch → nil OAuthClient → segfault at oauth_client provider.go:213. | CRITICAL, nil-deref, S119, S122c |
| `users-me-synthetic-tf-user` | `organization.go::handleUsersMe` | `handlers/organization_test.go` | GET `/api/v2/users/me` must return a synthetic terraform admin user with a stable non-empty `id` and a non-empty `division.id`. | S122d |
| `authorization-subject-grants-non-nil` | `organization.go::handleAuthorizationSubject` | `handlers/organization_test.go` | GET `/api/v2/authorization/subjects/{id}` must echo `id`, return a non-empty `name`, and a non-nil `grants` array. Provider iterates `grants` during user_roles diff. | S122f |
| `subjects-bulkadd-bulkremove-204` | `user_subresources.go::handleSubjectBulkadd` | `handlers/organization_test.go` | POST `/api/v2/authorization/subjects/{id}/bulkadd` and `/bulkremove` must each return HTTP 204. Body is intentionally not persisted; the provider gates only on the status code. | S122g |
| `users-create-default-division` | `user.go::handleUserCreate` | `handlers/identity_test.go` | POST `/api/v2/users` with no `division` field must round-trip a non-empty `division.id` on create + read-after-create. Without it the provider's `readUser` deref of `*currentUser.Division.Id` (resource_genesyscloud_user.go:166) segfaults. | nil-deref, S116c |
| `users-search-results-key` | `user_subresources.go::handleUserSearch` | `handlers/identity_test.go` | POST `/api/v2/users/search` response must use the paged-list key `results` (NOT `entities`). Provider's `getDeletedUserId` reads `Usersearchresponse.Results`; the wrong key silently returns zero matches and the destroy retry-loops to timeout. | S116c |
| `user-roles-get-version-and-roles` | `user_subresources.go::handleUserRolesGet` | `handlers/identity_test.go` | GET `/api/v2/users/{userId}/roles` must return a Userauthorization body with non-nil top-level `version` and `roles[]`. | S122d |
| `user-roles-put-echoes-ids` | `user_subresources.go::handleUserRolesPut` | `handlers/identity_test.go` | PUT `/api/v2/users/{userId}/roles` with a JSON array of role-id strings must respond 200 and echo each id in the response `roles[].id` shape. | S122d |
| `user-password-204` | `user_subresources.go::handleUserPassword` | `handlers/identity_test.go` | POST `/api/v2/users/{userId}/password` with any JSON body must return HTTP 204. | S122g |
| `group-members-individuals-round-trip` | `group.go::handleGroupMembersAdd` | `handlers/identity_test.go` | POST `/api/v2/groups/{id}/members` with N member ids must cause GET `/api/v2/groups/{id}/individuals` to return entities of length N containing those ids. DELETE removes only the targeted users. | S122 |
| `group-voicemail-dual-paths` | `group.go::handleGroupVoicemail` | `handlers/identity_test.go` | Both `/api/v2/groups/{id}/voicemail` (legacy) and `/api/v2/voicemail/groups/{id}/policy` (modern) must respond to GET and PATCH — neither may 501. Provider tries both URLs. | S122 |
| `architect-datatable-create-default-division` | `architect_datatable.go::handleDatatableCreate` | `handlers/architect_test.go` | POST `/api/v2/flows/datatables` with no `division` must round-trip a non-empty `division.id` on create + read. Provider deref of `*datatable.Division.Id` (resource_genesyscloud_architect_datatable.go:121) must not nil-segfault. | nil-deref, S122b |
| `flow-jobs-upload-protocol` | `flow.go::handleFlowJobCreate` | `handlers/architect_test.go` | 3-step upload-job protocol: POST `/api/v2/flows/jobs` returns `presignedUrl` (pointing at a NO_PROXY host) + `id`; PUT to `presignedUrl` accepts any payload; GET `/api/v2/flows/jobs/{jobId}` returns `status: "Success"` + non-empty `flow.id`. | S122b |
| `responsemanagement-library-crud-round-trip` | `responsemanagement.go::handleLibraryCreate` | `handlers/architect_test.go` | POST `/api/v2/responsemanagement/libraries` returns 200 with non-empty `id`; subsequent GET returns the stored library. | S122c |
| `routing-queue-create-200-with-membercount` | `routing_queue.go::handleRoutingQueueCreate` | `handlers/routing_test.go` | POST `/api/v2/routing/queues` must return HTTP 200 (not 201) — provider gates on 200. Subsequent GET `/api/v2/routing/queues/{id}` must include a non-nil integer `memberCount`. | S116c |
| `oauth-token-basic-auth` | `oauth.go::handleOAuthToken` | `handlers/oauth_test.go` | POST `/oauth/token` with `grant_type=client_credentials` and HTTP Basic Auth header (no client_id/client_secret in form body) returns 200 with bearer access_token (RFC 6749 §2.3.1). | S116c |

## Excluded layers (intentional)

- **S116 (TLS MITM proxy)** + **S116b (CA persistence)**: pure infrastructure. Covered by `handlers/tls_mitm_test.go`. Not wire-shape contracts.
- **S116c sub-paths** without an existing CRITICAL/MUST or nil-deref signal (authorization/divisions, organizations/me, voicemail userpolicies, routing utilization, routingskills, routinglanguages): basic-shape coverage is in existing tests; no documented invariant the provider crashes on if shape drifts. Revisit if a future sweep surfaces one.

## Adding a new contract (durable process)

1. Identify the wire-shape invariant the consuming provider depends on.
2. Add `// CRITICAL[<kebab-case-id>]: <one-line explanation + why it crashes if violated>` above the handler.
3. Add `TestContract_<id_with_underscores>` in the appropriate test file. The test must fail if the invariant is reverted.
4. Append a row to this matrix.

The S127 contract audit (`handlers/contract_audit_test.go`) will refuse to pass CI if any of those four steps is skipped.
