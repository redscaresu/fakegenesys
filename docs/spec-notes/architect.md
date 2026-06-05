# Spec notes — architect / responsemanagement / IDP (S111)

Source: `specs/genesys-openapi.json`. The hardest slice: `flow` brings
multipart upload + lock/publish state machine; `architect_datatable`
brings nested rows; `idp_generic` is a singleton.

## architect_datatable — `/api/v2/flows/datatables`

⚠ **Route ordering**: `/flows/datatables` is registered BEFORE
`/flows/{flowId}` so chi matches the static `datatables` segment
first.

| Method | Path |
|---|---|
| POST | `/api/v2/flows/datatables` |
| GET | `/api/v2/flows/datatables` |
| GET | `/api/v2/flows/datatables/{datatableId}` |
| PUT/PATCH | `/api/v2/flows/datatables/{datatableId}` |
| DELETE | `/api/v2/flows/datatables/{datatableId}` |
| POST | `/api/v2/flows/datatables/{datatableId}/rows` |
| GET | `/api/v2/flows/datatables/{datatableId}/rows` |
| GET | `/api/v2/flows/datatables/{datatableId}/rows/{rowId}` |
| PUT | `/api/v2/flows/datatables/{datatableId}/rows/{rowId}` |
| DELETE | `/api/v2/flows/datatables/{datatableId}/rows/{rowId}` |

Required: `name`. Rows use the row's `key` field as the row ID (unique
per datatable). fakegenesys does NOT enforce per-cell type checks
against the parent `schema` field (Reverse Fidelity: the spec doesn't
declare a per-cell error code; the real provider validates client-side).

FK cascade: deleting a datatable purges its rows.

## architect_user_prompt — `/api/v2/architect/prompts`

| Method | Path |
|---|---|
| POST | `/api/v2/architect/prompts` |
| GET | `/api/v2/architect/prompts` |
| GET | `/api/v2/architect/prompts/{promptId}` |
| PUT | `/api/v2/architect/prompts/{promptId}` |
| DELETE | `/api/v2/architect/prompts/{promptId}` |

Required: `name` (unique). 409 on duplicate.

**Deferred**: per-language resources (audio uploads) at
`/api/v2/architect/prompts/{promptId}/resources/{languageCode}`. The
prompt envelope itself is all the smoke harness needs. Adds when a
scenario requires audio.

## flow — `/api/v2/flows`

| Method | Path |
|---|---|
| POST | `/api/v2/flows` |
| GET | `/api/v2/flows` |
| GET | `/api/v2/flows/{flowId}` |
| PUT | `/api/v2/flows/{flowId}` (accepts `application/json` OR `multipart/form-data`) |
| DELETE | `/api/v2/flows/{flowId}` |
| POST | `/api/v2/flows/actions/checkout?flow={flowId}` (lock) |
| POST | `/api/v2/flows/actions/checkin?flow={flowId}` (save edits, release lock) |
| POST | `/api/v2/flows/actions/publish?flow={flowId}` (transition → published) |
| POST | `/api/v2/flows/actions/unlock?flow={flowId}` (force-release lock) |
| POST | `/api/v2/flows/actions/revert?flow={flowId}` (alias of unlock) |
| POST | `/api/v2/flows/actions/deactivate?flow={flowId}` (alias of unlock) |

Required: `name` (unique). State machine:

```
   create
     ↓
unpublished ──checkout──→ locked ──checkin/unlock──→ unpublished
                            │
                          publish
                            ↓
                        published
```

**Multipart upload**: PUT with `multipart/form-data` body. fakegenesys
reads the first file part and persists its contents opaquely as
`body.multipartContent` + filename as `body.multipartFilename`.
Subsequent GET returns both, so the Terraform provider's drift check
sees the same content it uploaded.

`Content-Type: application/json` PUT does a top-level merge (mirrors
the other handlers).

**No YAML interpretation**: fakegenesys deliberately doesn't parse the
flow YAML — the real provider supplies whatever shape the real Genesys
interpreter accepts. Reverse Fidelity.

## responsemanagement_response — `/api/v2/responsemanagement/responses`

| Method | Path |
|---|---|
| POST | `/api/v2/responsemanagement/responses` |
| GET | `/api/v2/responsemanagement/responses` |
| GET | `/api/v2/responsemanagement/responses/{responseId}` |
| PUT | `/api/v2/responsemanagement/responses/{responseId}` |
| DELETE | `/api/v2/responsemanagement/responses/{responseId}` |

Required: `name`. Body is opaque (rich-text content).

## idp_generic — `/api/v2/identityproviders/generic` (singleton)

| Method | Path |
|---|---|
| GET | `/api/v2/identityproviders/generic` |
| PUT | `/api/v2/identityproviders/generic` |
| DELETE | `/api/v2/identityproviders/generic` |

Singleton — no `id` in URL. Initial GET returns 404 (not configured).
PUT installs/replaces. DELETE removes (subsequent GET → 404).

## Topology notes (S114-T6)

- `architect_datatable_rows.datatable_id` → `architect_datatables.id`
- `flow.lockedUser.id` → `users.id` (when locked)
- `idp_generic` and `routing_utilization` are singletons → topology
  derivation treats them as scalar nodes, not graph-bearing.
