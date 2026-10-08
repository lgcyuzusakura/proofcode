# Project data gateway

ProofCode data operations use a project-scoped, reviewable intermediate representation (IR). The runner can inspect an allowlisted schema, compile a bounded plan, and submit it for approval. A plan digest binds task, project, attempt, resource, provider, environment, schema snapshot, and canonical IR. Changing any of those inputs invalidates the plan.

## Endpoints

Runner authentication (`/internal`, runner bearer token):

- `GET /internal/tasks/{taskId}/data/resources?attempt=N`
- `POST /internal/tasks/{taskId}/data/schema` with `{attempt,resourceId}`
- `POST /internal/tasks/{taskId}/data/plans` with `{attempt,resourceId,schemaVersion,ir,idempotencyKey}`
- `GET /internal/tasks/{taskId}/data/plans/{planId}?attempt=N`
- `POST /internal/tasks/{taskId}/data/plans/{planId}/explain` with `{attempt,digest}`
- `POST /internal/tasks/{taskId}/data/plans/{planId}/execute-approved` with `{attempt,digest}`

The last endpoint never accepts an approval or capability from the runner. It locks the task then the operation, verifies the persisted user approval and digest, creates a random capability inside the control plane, stores only its hash, consumes it once, and starts the external operation. The generic task approval path calls `DataOperationService.approveFromTool` for the `data_execute` tool, so the model cannot approve its own plan.

Every runner route requires its explicit task attempt, including plan inspection and EXPLAIN. An expired runner cannot infer a current attempt from a newer plan ID.

User authentication (`/api`, user bearer token):

- `POST/GET /api/projects/{projectId}/data/resources`
- `GET /api/projects/{projectId}/data/plans`
- `GET /api/projects/{projectId}/data/audit`
- `POST /api/projects/{projectId}/data/plans/{planId}/approval` with `{digest,approved}`
- `POST /api/projects/{projectId}/data/plans/{planId}/execute` with the short-lived capability returned by approval
- `POST /api/projects/{projectId}/data/plans/{planId}/compensation` for a separately approved Redis CAS compensation

## Secrets

Resource registration stores only an environment property reference such as `PG_DEV`. The server resolves `DATA_SECRET_PG_DEV` at execution time. PostgreSQL secrets are JSON containing `url`, `user`, and `password`; Redis secrets contain `host`, `port`, optional `database`, `username`, `password`, and `tls`. Redis recovery snapshots require a 32-byte base64 `DATA_SNAPSHOT_KEY` and are AES-256-GCM encrypted.

## Safety and scope

PostgreSQL supports parameterized SELECT and bounded INSERT/UPDATE/DELETE. Identifiers must be allowlisted by an inspected schema snapshot; UPDATE/DELETE require a nonempty filter, and every mutation requires an exact `expectedRows` count. Reads cap at 1,000 rows, 256 KiB, and a ten-second statement timeout. No raw SQL, joins, functions, DDL, stored procedures, or cross-store transaction is exposed yet. Database triggers that perform external side effects remain outside transaction rollback guarantees.

Redis supports namespaced string `get`, `set`, `delete`, `expire`, and bounded cursor `scan`. Raw commands, scripts, `KEYS`, `FLUSH*`, and cross-project keys are rejected. Redis compensation is a compare-and-set restoration of one encrypted snapshot. It is not a global rollback: expiration, external rewrites, and providers with side effects can produce a conflict or unknown outcome.

The ledger writes an audit row before and after execution. It stores only counts, hashes, status, and bounded error codes; fetched query rows and cache values are not persisted in plan or audit records. Caller-authored IR values remain in the reviewable plan. `COMMIT_UNKNOWN` and `COMPENSATION_UNKNOWN` are terminal for automatic replay and require operator reconciliation. A process crash after `EXECUTION_STARTED` leaves `EXECUTING`; this also blocks replay and requires reconciliation. Cancellation guarantees no mutation before the execution ledger is started; it cannot retract a transaction already sent to an external provider. Redis WATCH detects changes during execution; snapshot comparison cannot detect an earlier external rewrite that recreated an identical value and expiration.

## Verification

The normal suite verifies refusal before approval (even with runner auto-write flags), scope and digest checks, expired capabilities and attempts, duplicate execution, unknown commit handling, audit redaction, and task approval integration. H2 JDBC tests execute actual parameterized DML and prove affected-row mismatch rollback. `LiveDataAdapterTest` opts in with `PROOFCODE_LIVE_DATA_TEST=true`; it expects disposable PostgreSQL at port 15491 and Redis at 16491, using a test-only password. It verifies real PostgreSQL introspection, EXPLAIN, commit and rollback, plus Redis encrypted snapshots, namespace isolation, cursor scans, stale CAS refusal and compensation. The temporary services must not be production resources.
