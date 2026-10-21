# Verification status — 21 October 2026

This report distinguishes local checks from real service tests. It does not inherit the verification status of the separate Python Support Copilot project.

## Completed locally

Environment: Windows, Go 1.26.2. Commands:

```sh
go test ./... -count=1 -cover
go vet ./...
go build -o bin/inbox.exe ./cmd/inbox
go mod verify
node --check internal/inbox/web/app.js
```

All passed. The `internal/inbox` package reported **77.0% statement coverage**. The command entry points are compiled but not covered by that package's tests. A separate `GOOS=linux GOARCH=amd64 CGO_ENABLED=0` cross-build also succeeded; the Linux binary has not been run on a production host.

The publication candidates were checked against configured local secret values, private cloud identifiers, and common credential patterns. No matches were found. The local `.env`, build outputs, and Go caches are ignored by Git. This check does not replace the final scan after live testing.

The tests verify:

- Agent/reviewer authorization, host/origin checks, request limits, and sanitized settings.
- Duplicate submissions and changed content with a reused request ID.
- Blocking execution before approval and after rejection.
- Concurrent execution requests resulting in one simulated GitHub creation call.
- Editing an approved draft revokes approval; earlier approved content stays in history.
- Provider refusal, truncation, malformed output, HTTP failures, and blocked redirects.
- Claude and OpenAI request/response contracts using local HTTP test servers.
- Schema compatibility checks without overwriting an existing schema.
- OriginChainDB row and SQL adapter contracts using a local HTTP test server.
- Persistence failure before execution, ambiguous persistence, and failure to record an external result.
- Simulated GitHub timeout followed by a fresh service instance: no automatic duplicate action; explicit reconciliation verifies an existing issue.
- Manual proposals, disabled execution, and configuration changes to the destination.

These are tests with simulated upstreams. They are not evidence that a live model, cloud account, or GitHub token works.

## Browser workflow

Chrome was exercised against an explicit in-memory test harness, which is compiled only during tests. Settings identified **In-memory test fixture**; AI and GitHub credentials were absent, and execution was disabled.

Observed through the browser DOM: reviewer login, inbox selection, original report/draft display, manual proposal creation, approval, editing an approved draft back to `proposed`, saved revision history, rejection, filtering, connection settings, and workspace locking. No real issue was created.

Screenshot capture repeatedly timed out in the browser integration. Visual screenshot review and responsive viewport verification remain incomplete; no screenshot is presented as proof of a live cloud connection.

## Pending real-service verification

| Check | Status |
| --- | --- |
| OriginChainDB Cloud Free schema / rows / SQL / restart | Pending existing instance API key |
| Live Claude structured draft | Pending Claude API key and an account-available model ID |
| Live OpenAI structured draft | Not run; optional provider adapter only tested locally |
| Approved real GitHub issue and reconciliation | Not run |
| GitHub publication / CI result | Not published yet |
| Linux race detector | Not run locally; Windows host lacks a configured C compiler |
| Container build / production deployment | Not run; Docker daemon unavailable |

The CI workflow is configured to run the race detector on Linux after publication. A configured workflow is not a successful CI run.

## Reproduce the cloud test

Prepare the ignored root `.env` with the real Cloud Free endpoint, tenant, existing instance key, application access keys, and repository configuration. Confirm **Free** in the cloud console separately; the data API test cannot prove the account's billing plan.

PowerShell:

```powershell
$env:INBOX_LIVE_TEST = '1'
go test ./internal/inbox -run '^TestCloudFreeLive$' -v -count=1 -timeout 6m
```

Shell:

```sh
INBOX_LIVE_TEST=1 go test ./internal/inbox -run '^TestCloudFreeLive$' -v -count=1 -timeout 6m
```

The test writes synthetic records into a fresh `inbox_test_...` namespace. It verifies schema registration/reuse, row updates, duplicate submission, SQL listing, state recovery through a fresh client, and rejection. It retains its test rows and never deletes existing data. It does not create a GitHub issue.

For one real AI draft as well, set `INBOX_LIVE_AI_TEST=1`, `AI_PROVIDER=claude`, `AI_MODEL`, and `ANTHROPIC_API_KEY`. This makes a real billable provider request, saves the draft, then rejects the synthetic action. A successful schema/JSON parse still does not establish factual correctness: inspect the draft against the report.

For the external side effect, use the running app with execution enabled on a designated demo repository, review a synthetic draft, approve it, execute once, and inspect the created issue. Record the issue URL and tested commit here after success. Verify a rejected proposal produces no issue and that history survives a process restart.

Before publication, remove temporary development credentials, inspect the staged files and history for secrets, update this report with observed results, and then publish. Do not mark pending rows as passed based on tests with simulated services.
