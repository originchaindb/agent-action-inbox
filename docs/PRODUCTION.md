# Production setup for an internal review tool

The production target is one small Go service behind HTTPS, connected to an OriginChainDB cloud database and one configured AI provider. The database does not host the Go application. These deployment templates have not yet been exercised on a production host; see [verification status](VERIFICATION.md).

## Deployment shape

Run **one process per namespace**. Do not use multiple replicas, rolling deployments with overlapping old/new processes, or multiple developer machines against the same namespace. The process mutex and version checks do not coordinate independent writers. Adding distributed compare-and-swap/leases and testing crash recovery is required before scaling writers.

Use Cloud Free for the development proof. Before using real client reports, verify the database plan's capacity, availability, backup/restore, and retention provisions against your requirements. The starter makes no Free-tier SLA promise. AI requests, host compute, and network traffic may have separate costs. Set provider spend/rate limits for the project's API key.

## Configuration and data

Create a production-specific database namespace and secrets outside the repository:

```dotenv
APP_ADDR=127.0.0.1:8087
APP_ORIGIN=https://inbox.example.com
OC_BASE_URL=https://YOUR_INSTANCE_ORIGIN
OC_TENANT=YOUR_TENANT
OC_NAMESPACE=agent_inbox_prod
OC_BEARER=YOUR_INSTANCE_KEY
AI_PROVIDER=claude
AI_MODEL=YOUR_AVAILABLE_STRUCTURED_OUTPUT_MODEL
ANTHROPIC_API_KEY=YOUR_API_KEY
AI_REQUEST_LIMIT=3
AI_REQUEST_LEDGER=/var/lib/agent-action-inbox/ai-requests.json
REVIEWER_TOKEN=YOUR_RANDOM_REVIEWER_SECRET
AGENT_TOKEN=YOUR_DIFFERENT_RANDOM_AGENT_SECRET
GITHUB_REPOSITORY=YOUR_OWNER/YOUR_REPOSITORY
GITHUB_TOKEN=YOUR_REPOSITORY_SCOPED_TOKEN
ENABLE_GITHUB_EXECUTION=false
```

Replace every placeholder. Use a secrets manager or a server-owned environment file readable only by the service administrator. Keep both app access keys separate; do not put the reviewer key into the AI agent's environment. Restrict the GitHub token to Issues read/write on the selected repository. Add `OC_DB_USER` and `OC_DB_KEY` together only if the database connection requires them.

The report is sent to the chosen model provider. The original report, generated draft, reviewer notes/revisions, and result are stored in OriginChainDB. Approved issue contents are sent to GitHub. Decide what information is appropriate for each destination before submitting real reports. The app does not redact sensitive input automatically.

The durable request ledger reserves one attempt before a provider call. Failed calls retain their reservation. Keep it across restarts and deployments; the default limit of three is for initial verification. An unreadable ledger or an existing `.lock` blocks calls. After a crash, stop all writers and inspect the ledger and provider usage before an operator removes a stale lock. Never delete/reset the counter as an automatic recovery step. This cap covers this application only. For a billing cap, configure a dedicated Claude workspace's [spend limit](https://platform.claude.com/docs/en/manage-claude/workspaces).

In the browser, only the app reviewer access key is entered. It is held in JavaScript memory, not localStorage or cookies. Cloud, provider, and GitHub keys stay on the server. Configuration changes require a restart. Rotate credentials through their owning services, update server secrets, and restart without overlapping processes.

## Build and run on Linux

From the reviewed checkout on the target architecture:

```sh
go mod verify
go test ./...
go vet ./...
CGO_ENABLED=0 go build -trimpath -o inbox ./cmd/inbox
```

Create an unprivileged `agent-inbox` service account. Install the binary at `/opt/agent-action-inbox/inbox`, make it executable, and save the environment as `/etc/agent-action-inbox.env` with restrictive permissions (for example root-owned mode `0600`, read by systemd). Do not grant the service write access to its executable or unit file.

Install [deploy/inbox.service](../deploy/inbox.service) under `/etc/systemd/system/`, then:

```sh
sudo systemctl daemon-reload
sudo systemctl enable --now inbox
sudo systemctl status inbox
```

The unit shuts down with SIGINT and allows the in-flight execution/result recording window to finish. A forced stop can leave an action in `executing`; reconcile it after restart. For upgrades, stop the old service completely before starting the new binary. Keep the previous binary for rollback, and never roll back schema expectations without checking compatibility.

## HTTPS and access

Point a domain you control at the host and configure TLS with your existing reverse proxy. [deploy/Caddyfile.example](../deploy/Caddyfile.example) gives a minimal Caddy example. Install and run Caddy using its [official service guidance](https://caddyserver.com/docs/running), replace `inbox.example.com`, validate your configuration, and reload Caddy.

Keep port 8087 private. Preserve the original HTTP Host header: the application validates it against `APP_ORIGIN` and rejects mismatches. Do not rewrite Host to `localhost`. Do not add automatic retries for POST requests. Configure gateway connection/body/rate limits appropriate to a small internal service, and keep request timeouts long enough for the bounded synchronous provider/database calls. See Caddy's [reverse proxy reference](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy).

Restrict access using your VPN or organization access gateway. The built-in bearer key distinguishes reviewer versus agent, but all reviewers share a role. It does not provide SSO, per-person accountability, user management, or tenant isolation. Add those before offering this as a shared customer-facing product. Do not forward a reviewer credential automatically to arbitrary callers at the gateway.

## Optional container

The Dockerfile compiles a static Go binary, copies CA certificates, and runs as an unprivileged numeric user. It does not copy `.env` files into the image.

```sh
docker build -t agent-action-inbox:reviewed .
docker run --name agent-action-inbox --stop-timeout 145 \
  --read-only --cap-drop ALL --security-opt no-new-privileges \
  --env-file /etc/agent-action-inbox.env \
  -e APP_ADDR=0.0.0.0:8087 \
  -e AI_REQUEST_LEDGER=/state/ai-requests.json \
  -v agent-inbox-state:/state \
  -p 127.0.0.1:8087:8087 \
  agent-action-inbox:reviewed
```

Use one container, not this container plus the systemd application. Keep `APP_ORIGIN` equal to the public HTTPS origin. Set an adequate stop timeout (at least 145 seconds) and send SIGINT when stopping the container, or use the image's configured stop signal. Container administrators can inspect environment secrets; a trusted host remains part of the boundary.

## Validate before enabling execution

1. Unauthenticated `/api/actions` returns 401. An agent key cannot approve or execute.
2. Settings reports the real cloud database connected and the intended repository.
3. A synthetic Claude draft reaches `proposed`; missing evidence remains visible.
4. Editing an approved draft returns it to `proposed`.
5. Enable execution and create one explicitly synthetic issue in the designated demo repository. Compare its body with the approved revision.
6. Reject another proposal and verify no external action occurs. Restart the app and verify both histories remain.
7. After simulating a timeout in staging, ensure the system requires reconciliation instead of a second creation attempt.

Record the tested model ID, database plan, application commit, synthetic issue URL, and observed results without tokens, private endpoint IDs, or full provider payloads. Remove temporary test credentials from the development checkout before publishing it.

## Operating limits and recovery

- `/healthz` is process liveness. Use authenticated settings checks and a separate synthetic monitor for readiness; do not run a paid model call on every health probe.
- A provider refusal, truncation, or malformed output becomes `draft_failed`. A missing API key prevents the request. No automatic fallback or retries can incur another model charge.
- A database failure before persisting execution intent blocks the GitHub call. A GitHub failure or a lost response after creation is uncertain; inspect the repository before any replacement.
- Reconciliation reads the selected issue and checks the configured repository and action marker. It never posts another issue. A marker is a correlation aid, not cryptographic proof; trusted repository writers can modify issue bodies.
- A restored database snapshot may precede an external GitHub action. Disable execution during restores, inspect affected actions against GitHub, and resolve the gap before re-enabling it. Database state and GitHub cannot be committed in a single transaction.
- History is stored inside the action row, including draft snapshots for review decisions. It is not append-only or tamper-evident. Database administrators can change it. Export/retain records according to your operational policy; no retention sweeper is included.
- The service lists the latest 50 summaries and serializes mutations. It is intended for a small review queue, not a bulk job runner. Rate-limit agent submissions at the gateway and monitor database usage.
- Normal application logs exclude reports, keys, provider bodies, and issue bodies. Make sure your proxy and monitoring configuration do not record Authorization headers or submitted content.

The project deliberately does not claim exactly-once delivery. It provides a review gate and conservative recovery behavior for a single writer.
