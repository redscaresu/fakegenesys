# Spec notes — routing resources (S110)

Source: `specs/genesys-openapi.json`.

Five resources. Verb-set varies (some have PATCH, some PUT, languages is read-only after create, utilization is a singleton).

## routing_queue — `/api/v2/routing/queues`

| Method | Path |
|---|---|
| POST | `/api/v2/routing/queues` |
| GET | `/api/v2/routing/queues` |
| GET | `/api/v2/routing/queues/{queueId}` |
| PUT | `/api/v2/routing/queues/{queueId}` |
| DELETE | `/api/v2/routing/queues/{queueId}` |
| GET | `/api/v2/routing/queues/{queueId}/members` |
| POST | `/api/v2/routing/queues/{queueId}/members` (add) |
| PATCH | `/api/v2/routing/queues/{queueId}/members` (idempotent set replace) |

Required: `name` (unique). 409 on duplicate.

**Members semantics**: PATCH is **idempotent set replace** — the request body is the complete desired member set; the server wipes the existing set and re-inserts. POST adds without removing. The Terraform provider uses PATCH for sets it manages from HCL.

**FK cascade**: deleting a queue cascades to `routing_queue_members` via the schema-level FK. fakegenesys does NOT pre-block destruction; the cascade handles it.

## routing_skill — `/api/v2/routing/skills`

| Method | Path |
|---|---|
| POST | `/api/v2/routing/skills` |
| GET | `/api/v2/routing/skills` |
| GET | `/api/v2/routing/skills/{skillId}` |
| PATCH | `/api/v2/routing/skills/{skillId}` |
| DELETE | `/api/v2/routing/skills/{skillId}` |

Required: `name` (unique). 409 on duplicate.

## routing_wrapupcode — `/api/v2/routing/wrapupcodes`

| Method | Path |
|---|---|
| POST | `/api/v2/routing/wrapupcodes` |
| GET | `/api/v2/routing/wrapupcodes` |
| GET | `/api/v2/routing/wrapupcodes/{codeId}` |
| PUT | `/api/v2/routing/wrapupcodes/{codeId}` |
| DELETE | `/api/v2/routing/wrapupcodes/{codeId}` |

Required: `name` (unique).

## routing_language — `/api/v2/routing/languages`

| Method | Path |
|---|---|
| POST | `/api/v2/routing/languages` |
| GET | `/api/v2/routing/languages` |
| GET | `/api/v2/routing/languages/{languageId}` |
| DELETE | `/api/v2/routing/languages/{languageId}` |

**No PUT or PATCH** — the spec declares neither, so fakegenesys returns 501 on those methods (Reverse Fidelity).

## routing_utilization — `/api/v2/routing/utilization` (singleton)

| Method | Path |
|---|---|
| GET | `/api/v2/routing/utilization` |
| PUT | `/api/v2/routing/utilization` |
| DELETE | `/api/v2/routing/utilization` |

No `id` in URL. fakegenesys keeps one row keyed by `_`. Initial GET (before any PUT) returns `{"utilization":{}}` — the documented default. DELETE returns to default.

PUT body is opaque (per-media-type capacity maps). fakegenesys does NOT validate media-type strings — the spec allows any string under `utilization.<mediaType>` and the real provider enforces its own list.

## Topology notes (S114-T6)

These shapes drive infrafactory's topology derivation:

- `routing_queue` → `routing_queue_members.user_id` (members) → `users.id`
- `routing_queue.skills[*].id` → `routing_skills.id`
- `routing_queue.wrapupCodes[*].id` → `routing_wrapupcodes.id`
- `routing_queue.languages[*].id` → `routing_languages.id`
- `routing_queue.acwSettings.wrapupPrompt` → not graph-bearing
