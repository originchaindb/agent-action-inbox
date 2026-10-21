# API

Base URL in development: `http://127.0.0.1:8087`. Use HTTPS outside loopback. All `/api/` routes require a bearer access key. All writes require `Content-Type: application/json`; request bodies are limited to 32 KiB. Cross-origin browser calls are rejected. Secrets belong in headers, never URLs.

## Submit a proposal

Use the agent access key. Keep the same request ID when recovering from a lost response. Reusing it with changed content returns HTTP 409. A new request ID means a new proposal and, when AI drafting is used, potentially another billable request.

```sh
curl http://127.0.0.1:8087/api/actions \
  -H "Authorization: Bearer $AGENT_TOKEN" \
  -H 'Content-Type: application/json' \
  --data-binary @examples/ai-proposal.json
```

The synchronous response is the action, including `id`, `version`, `status`, the draft, and events. A provider error is recorded as `draft_failed`; inspect `status` even when the HTTP response is 200. The returned action must remain unapproved until the reviewer decides.

If your agent already has a draft, use [manual-proposal.json](../examples/manual-proposal.json). This does not call Claude or OpenAI.

## Reviewer routes

| Method and path | Purpose |
| --- | --- |
| `GET /api/actions` | Latest 50 action summaries |
| `GET /api/actions/{id}` | Full action and saved review history |
| `GET /api/settings` | Sanitized configuration and a database read probe |
| `POST /api/actions/{id}/edit` | Save title/body and revoke any earlier approval |
| `POST /api/actions/{id}/approve` | Approve the supplied title/body as a specific saved revision |
| `POST /api/actions/{id}/reject` | Reject an unexecuted proposal |
| `POST /api/actions/{id}/execute` | Attempt one GitHub creation for an approved action |
| `POST /api/actions/{id}/reconcile` | Verify an existing GitHub issue after an uncertain attempt |

These require `REVIEWER_TOKEN`, which must not be given to the proposing agent. Reviewer writes include the current `version`, so an old tab cannot overwrite a newer review.

Approval body:

```json
{
  "version": 2,
  "title": "Filtered export includes rows outside the date range",
  "body": "Observed behavior: ...\n\nMissing information: ...",
  "note": "Reviewed the reproduction and removed an unsupported claim."
}
```

Execution body: `{"version":3}`. Reconciliation body: `{"version":5,"issue_number":12,"note":"Checked the existing issue."}`. These versions are illustrative; always use the current value from the API. A 409 means refresh and inspect the action rather than blindly retrying.

The `GET /healthz` route is unauthenticated and reports process liveness only. It is not a database or AI readiness check.

## State transitions

```text
drafting → proposed → approved → executing → succeeded
    │          │         │           └──→ uncertain
    ↓          └──→ rejected             │
draft_failed       ↑                    └──→ succeeded (verified reconciliation)
    └──────────────┘

approved → proposed (editing revokes approval)
executing → succeeded (reconciliation after a process interruption)
```

An interrupted `drafting` record is not automatically regenerated. Reject it and deliberately submit a new request if desired. An `executing` or `uncertain` record cannot be reset or executed again through this API.

## Storage contract

`{OC_NAMESPACE}.actions_v1` uses an `id` primary key. Columns `created_ms` and `updated_ms` are `i64`; `id`, `payload`, `input_hash`, `status`, `title`, and `repository` are strings. `payload` contains the full action JSON and its history; the other columns support small inbox summary queries.

The service checks an existing manifest before registering a missing schema. It never migrates an incompatible schema automatically. Use a new namespace for an incompatible application version, and plan any migration explicitly.

Every mutation is serialized inside one process. `version` is an application check, not a database compare-and-swap guarantee. Request IDs are hashed to stable record IDs; the full input hash detects changed content using the same key. Edits and approvals retain the corresponding draft in their history event. Review revisions are bounded to keep records small.

The database adapter and external integrations omit upstream response bodies from errors, reject redirects, and do not use ambient HTTP proxy settings. They do not retry network requests automatically. Read and inspect current state after connectivity failures.
