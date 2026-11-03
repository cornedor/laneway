# Timesheet API

laneway logs time to Jira's worklogs by default. With `timesheet: provider: http` it logs to any
server that speaks the small JSON API below instead: a time registration tool of your own, or an
adapter in front of one. One provider is active at a time.

```yaml
timesheet:
  provider: http
  url: http://127.0.0.1:29174/laneway   # the API's root, without /v1
  token_cmd: [pass, show, timesheet]    # optional: a bearer token (or token:)
```

The timer (`T`), the timesheet (`W`), the week view and proposals then read and
write through the server. When the server runs a timer itself (`serverTimer`), laneway's timer is
that one: started in laneway it runs on the server, started elsewhere it shows in laneway.

## Conventions

- JSON in and out. Times are RFC 3339 with an offset; durations are whole seconds. A server
  that stores minutes may round; laneway reads the entries again after each write.
- Requests carry `Authorization: Bearer <token>` when one is configured.
- An error is any non-2xx status with `{"error": "what went wrong", "hint": "…", "url": "…"}`;
  `hint` and `url` are optional and shown with the error. `401`: the server needs a sign-in
  of its own (`url` is where); `404`: no such entry; `400`, `409`, `422`: refused.
- An entry the server accepted but has not stored yet is `pending`. Its `id` may be temporary:
  the server must accept that id on later calls (an edit or delete right after a create).

## Entry

```json
{
  "id": "425824",
  "key": "ABC-123",
  "summary": "Make the image configurable",
  "description": "first draft",
  "start": "2026-11-03T07:15:00Z",
  "seconds": 5400,
  "running": false,
  "pending": false
}
```

| Field | |
|---|---|
| `id` | the server's id, a string |
| `key` | the Jira issue key; `""` for an entry without an issue (`keyless`) |
| `summary` | the issue's title, when the server knows it |
| `description` | what you did; laneway's worklog comment |
| `start` | when the work started |
| `seconds` | time logged; for a running entry the time so far |
| `running` | the server's running timer |
| `pending` | accepted, not stored yet |

## Endpoints

All under the configured root.

### `GET /v1/info`

```json
{"name": "easyreg", "caps": {"serverTimer": true, "move": true, "keyless": true}}
```

`name` labels the provider in laneway. `caps`, each `false` when left out:

- `serverTimer`: the server runs a timer (`GET /v1/running`, `POST` without `seconds`).
- `move`: an entry's `key` can change (`PATCH` with `key`).
- `keyless`: entries without an issue exist.

### `GET /v1/entries?from=…&to=…`

Your entries that start in `[from, to)`, a running one included, by start:
`{"entries": [Entry, …]}`. laneway asks for a day or a week.

### `GET /v1/running`

The running entry, or `null`. Only with `serverTimer`. laneway polls it about twice a minute.

### `POST /v1/entries`

```json
{"key": "ABC-123", "description": "…", "start": "2026-11-03T09:00:00+01:00", "seconds": 1800}
```

Logs an entry; answers `201` with the Entry. Without `seconds` it starts the timer at `start`
(`serverTimer`), and a timer already running ends there.

### `PATCH /v1/entries/{id}`

Any of `key`, `description`, `start`, `seconds`; answers the Entry. `seconds` sets the end to
the start (the new one when `start` is given) plus `seconds`: on a running entry that stops it.
`key` moves the entry to another issue (`move`).

### `DELETE /v1/entries/{id}`

Answers `204`. laneway uses it for the timer's discard too (`ctrl+d` at its stop prompt).
