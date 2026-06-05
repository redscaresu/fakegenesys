# Spec notes — identity resources (S109)

Source: `specs/genesys-openapi.json` (Genesys Cloud Swagger 2.0).

Five resources. Each entry: endpoint surface + request/response shapes
+ pagination + error codes used by handlers in `handlers/{user,group,location,auth_role,oauth_client}.go`.

## user — `/api/v2/users`

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/v2/users` | Create. Required body fields: `name`, `email`. |
| GET | `/api/v2/users` | List with pagination. Query: `pageNumber`, `pageSize`. |
| GET | `/api/v2/users/{userId}` | Read single. |
| PATCH | `/api/v2/users/{userId}` | Partial update (top-level merge). |
| DELETE | `/api/v2/users/{userId}` | **Soft delete** — sets `state=deleted`, returns 204. Subsequent GET returns 200 with `state=deleted`. |

Response body shape: opaque JSON. fakegenesys persists what was POSTed plus computed fields:
- `id` (UUID, server-assigned)
- `selfUri` (`/api/v2/users/{id}`)
- `version` (int, bumped on PATCH)
- `dateCreated`, `dateModified` (RFC3339)
- `state` (`active` | `deleted`)

Errors:
- 400 — missing `name` or `email`.
- 404 — userId not present.
- 409 — email duplicates an existing user.

## group — `/api/v2/groups`

| Method | Path |
|---|---|
| POST | `/api/v2/groups` |
| GET | `/api/v2/groups` |
| GET | `/api/v2/groups/{groupId}` |
| PUT | `/api/v2/groups/{groupId}` |
| DELETE | `/api/v2/groups/{groupId}` |

Required body fields: `name`. Optional: `type` (default `official`), `description`, `rulesVisible`, `visibility`.

Hard delete (200 → 404 after). Errors: 400 missing name, 404 unknown id.

## location — `/api/v2/locations`

| Method | Path |
|---|---|
| POST | `/api/v2/locations` |
| GET | `/api/v2/locations` |
| GET | `/api/v2/locations/{locationId}` |
| PATCH | `/api/v2/locations/{locationId}` |
| DELETE | `/api/v2/locations/{locationId}` |

Required: `name`. Optional: `address`, `coordinates`, `notes`, `path`.

Hard delete.

## auth_role — `/api/v2/authorization/roles`

| Method | Path |
|---|---|
| POST | `/api/v2/authorization/roles` |
| GET | `/api/v2/authorization/roles` |
| GET | `/api/v2/authorization/roles/{roleId}` |
| PUT | `/api/v2/authorization/roles/{roleId}` |
| PATCH | `/api/v2/authorization/roles/{roleId}` |
| DELETE | `/api/v2/authorization/roles/{roleId}` |

Required: `name`. Permissions are well-known strings (e.g. `routing:queue:edit`); **fakegenesys does NOT validate the permission grammar** (per Reverse Fidelity: the spec doesn't pin the permission catalog). 409 on duplicate name.

## oauth_client — `/api/v2/oauth/clients`

| Method | Path |
|---|---|
| POST | `/api/v2/oauth/clients` |
| GET | `/api/v2/oauth/clients` |
| GET | `/api/v2/oauth/clients/{clientId}` |
| PUT | `/api/v2/oauth/clients/{clientId}` |
| DELETE | `/api/v2/oauth/clients/{clientId}` |

Required: `name`, `authorizedGrantType` (`CLIENT_CREDENTIALS` or `CODE`).

**Reveal-once secret semantics**: the create response (POST 201) includes a `secret` field. Subsequent GETs do NOT include the secret (mirrors real Genesys). To re-read, callers must hit `POST /api/v2/oauth/clients/{id}/secret` to mint a new one (not yet implemented in fakegenesys — adds when a scenario needs it).

## Pagination contract

All list endpoints share the response envelope:

```json
{
  "entities": [...],
  "pageCount":  3,
  "pageNumber": 1,
  "pageSize":   25,
  "total":      57,
  "firstUri":   "...",
  "selfUri":    "..."
}
```

Defaults: `pageNumber=1`, `pageSize=25`. Max `pageSize=200`.

## Error body

Genesys-shaped:

```json
{
  "status":     404,
  "code":       "not.found",
  "message":    "...",
  "contextId":  "..."  // optional
}
```

fakegenesys-emitted codes: `not.found`, `bad.request`, `conflict`, `authentication.required`, `authentication.invalid`, `not.implemented`, `internal`.
