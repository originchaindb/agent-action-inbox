# Verification results — 21 October 2026

**Real Claude + OriginChainDB Cloud Free + GitHub passed.** The Go app ran on a local Windows host against actual cloud APIs. No local database substituted for Cloud Free. The cloud console separately confirmed **Free**, **Running**, and **No charge**; the data API alone cannot establish the account's plan.

Evidence: [synthetic GitHub issue #1](https://github.com/originchaindb/agent-action-inbox/issues/1), [sanitized live receipt](live-verification.json), [inbox screenshot](live-inbox.jpg), and [decision history screenshot](live-history.jpg). All submitted reports were invented test data, with no customer information.

## Real-service checks

| Check | Observed result |
| --- | --- |
| Cloud Free schema / rows / SQL | Schema creation and reuse, row insert/update, duplicate request handling, and SQL listing passed |
| Claude structured draft | Two successful requests using `claude-haiku-4-5-20251001` |
| Browser review and GitHub execution | Edited and approved a real Claude draft, then created exactly one issue through the app |
| Approval integrity | GitHub title/body matched the saved approved revision, plus the action marker |
| Rejection and permissions | Rejected action could not execute; agent access could neither approve nor read the inbox |
| Duplicate protection | Reused request ID returned the original action; repeated execution was blocked; one demo issue observed |
| Process restart | Stopped and restarted the compiled server; completed/rejected actions and history remained |
| Lost-result reconciliation | Isolated simulated missing-result record reconciled using a real GitHub marker read and real Cloud Free persistence; no second issue creation |
| Persistent AI ceiling | The two reserved attempts remained after restart |
| OpenAI API | Adapter tested with local HTTP fixtures only; no live OpenAI call |
| Production host / container | Not deployed; Docker daemon unavailable on the development host |

The lost-result test deliberately copied the completed action into a fresh recovery namespace and removed its result there. The original action was unchanged. This verifies reconciliation against live services; it does **not** claim an actual network interruption was forced during issue creation.

The model suggested a reproduction sequence that the source report had not confirmed. The reviewer removed it and added an explicit synthetic-demo label before approval. Both revisions remain in the action history. Successful structured output does not establish factual correctness.

## Claude usage and limit

The two requests consumed **790 input tokens and 742 output tokens**. At the documented Haiku 4.5 rates of $1 per million input tokens and $5 per million output tokens, estimated token cost was **$0.0045**, below the requested **$10** ceiling. This is a token-cost estimate, not an invoice or tax calculation. [Model details](https://platform.claude.com/docs/en/models/haiku-4-5/overview), [pricing](https://platform.claude.com/docs/en/about-claude/pricing).

The app was configured for a persistent ceiling of **three provider attempts**, reserves attempts before sending requests, and never automatically retries them. Exactly two were used. The fixed model, bounded input/output, and limited requests kept this verification within budget. This request counter is **not** general dollar accounting and cannot control other clients using the same account. No account-wide billing setting was changed; configure a dedicated [Claude workspace spend limit](https://platform.claude.com/docs/en/manage-claude/workspaces) for ongoing billing enforcement.

## Local automated checks

Environment: Windows, Go 1.26.2.

```sh
go test ./... -count=1 -cover
go vet ./...
go build -o bin/inbox.exe ./cmd/inbox
go mod verify
node --check internal/inbox/web/app.js
```

All passed. `internal/inbox` reported **77.7% statement coverage**. Command entry points compile but are not covered by that package's tests. A Linux cross-build also succeeded; the Linux binary has not been deployed to a production host.

Focused tests cover role authorization, host/origin validation, sanitized settings, request/body limits, stable request IDs, stale reviews, edit-revokes-approval behavior, concurrent execution, provider refusals/truncation/malformed output, schema compatibility, database failures, uncertain external results, and reconciliation. Durable request-budget tests cover restarts, exhausted budgets, concurrent reservation, corrupt files, and lock failures. Claude/OpenAI and database adapter contract tests use local HTTP fixtures.

Linux CI runs `go test -race` as well as formatting, module verification, vet, build, JavaScript syntax, and private-file exclusion checks. See [workflow runs](https://github.com/originchaindb/agent-action-inbox/actions/workflows/ci.yml). The Windows host has no configured C compiler for the race detector.

## Browser evidence and boundaries

Chrome verified reviewer sign-in, real database connection settings, Claude model and three-attempt ceiling, draft review/edit/approval, completed issue link, history, rejection, and state after a process restart. The screenshots show actual application data from this test. Earlier UI checks also exercised a clearly labeled test-only in-memory harness; those are not the evidence for cloud connectivity.

Responsive viewport checks, production TLS/proxy deployment, backups, and container execution have not been verified. This remains a single-writer internal-tool starter, with shared reviewer-role credentials and no distributed transaction spanning GitHub and the database.

## Reproduce the cloud test

Prepare the ignored root `.env` using `.env.example`. Confirm **Free** in the cloud console separately. Use a fresh namespace, synthetic data, and your own provider/repository credentials.

PowerShell:

```powershell
$env:INBOX_LIVE_TEST = '1'
go test ./internal/inbox -run '^TestCloudFreeLive$' -v -count=1 -timeout 6m
```

Shell:

```sh
INBOX_LIVE_TEST=1 go test ./internal/inbox -run '^TestCloudFreeLive$' -v -count=1 -timeout 6m
```

The test writes and retains synthetic records in a fresh `inbox_test_...` namespace. It verifies schema registration/reuse, row updates, duplicate submission, SQL listing, state recovery through a fresh client, and rejection. It never creates a GitHub issue or deletes existing data.

For one real AI draft, also set `INBOX_LIVE_AI_TEST=1`, `AI_PROVIDER=claude`, `AI_MODEL`, and `ANTHROPIC_API_KEY`. This makes a billable request, persists the draft, and rejects the synthetic action. The project request ledger is shared with the app; retain it between runs.

For GitHub execution, run the app with your repository-scoped token and execution enabled. Review a synthetic proposal, approve it, execute once, and compare the created issue with the approved revision. Reject another action, verify duplicate execution is blocked, and restart the process to check retained state.

`TestLiveRecoveryFromExistingDemoIssue` is a separate explicit opt-in (`INBOX_LIVE_RECOVERY_TEST=1`). It requires access to the already-tested completed action referenced by `live-verification.json` and its GitHub issue. It writes only an isolated recovery fixture and performs a GitHub read. Adapt the receipt to your own test action when reproducing it; it is not a general public-account integration test.

Live tests are skipped by default. Ordinary tests and CI do not spend model credits, require credentials, or create cloud records. Temporary development credentials are removed before publication; `.env`, caches, binaries, and the local usage ledger are excluded from Git.
