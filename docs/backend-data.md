# Project data gateway / 项目数据工作台

ProofCode compiles bounded, reviewable Query IR into parameterized database operations. The same approval ledger serves a programming task and a standalone data management session. Resources, sessions, plans, schema snapshots, audits and recovery records are scoped to a project. The current provider implementations are PostgreSQL and standalone Redis strings.

数据库和缓存连接由项目管理；打开其他项目不能通过其资源 ID 读取或执行当前项目的计划。数据工作台可以创建独立管理会话，不需要启动编程任务。所有数据读取、修改和恢复操作均采用“生成计划、预览、用户批准、执行”的流程；元数据检查和只读 EXPLAIN 是单独的预览接口。

## Approval And Version Binding

Every plan digest binds the project, task and attempt or data session, resource ID, provider, environment, secret reference, schema allowlist, resource configuration version, policy version, schema snapshot and canonical IR. The `data-ir-v2` digest is recomputed at approval and execution. A PATCH increments `resourceVersion`, including when the reference string stays unchanged after credential rotation. Old approvals then fail and require a new plan.

The control plane locks context, operation and resource in that order, persists the user decision and a five-minute capability hash, then consumes it once and commits `EXECUTION_STARTED` before entering the provider execution path. A failure to persist the start audit means zero calls to `execute` or `compensate`. Runner credentials cannot call user approval routes. The model cannot approve its own operation, and runner auto-write flags do not bypass this ledger.

用户批准前，计划不会进入执行适配器。批准只适用于绑定的原始 IR，不能批准后替换 SQL 或参数。数据库元数据检查、连接验证及 EXPLAIN 可以在批准前发生；“批准前零执行”指查询计划、写入和补偿执行次数为零，不代表批准前禁止建立元数据连接。

## User API

All paths below are relative to `/api/projects/{projectId}/data` and require the user bearer token.

| Method | Path | Request |
| --- | --- | --- |
| POST / GET | `/resources` | register: `name?`, `provider`, `environment`, `secretRef`, `allowedSchema?` |
| PATCH | `/resources/{id}` | `expectedVersion`, optional `name`, `active`, `environment`, `secretRef`, `allowedSchema` |
| POST / GET | `/sessions` | create: `name`, optional `purpose=MANAGEMENT`, `expiresInMinutes=120` |
| GET | `/sessions/{id}` | session details |
| POST | `/sessions/{id}/close` | close without reactivating a programming task |
| POST | `/sessions/{id}/schema` | `resourceId` |
| POST | `/sessions/{id}/plans` | `resourceId`, `resourceVersion?`, `schemaVersion`, `ir`, `idempotencyKey` |
| GET | `/plans` or `/plans/{id}` | project-scoped plan / history |
| POST | `/plans/{id}/explain` | `digest`; PostgreSQL read Query IR only |
| POST | `/plans/{id}/approval` | `digest`, `approved` boolean |
| POST | `/plans/{id}/execute` | `digest`, approval `capability`, `attempt?` |
| POST | `/plans/{id}/compensation` | `idempotencyKey`; creates a new PENDING plan |
| GET | `/audit` | project-scoped audit history |

Providers are `postgres` and `redis`; environments are `dev`, `staging` and `production`. Session purposes are `MANAGEMENT` and `RECOVERY`, with expiry bounded to 5–1,440 minutes. Session plans have no task ID and use attempt zero, so their execute request can omit `attempt`. Task plans require their explicit current attempt. Closing or expiring a session blocks subsequent approval and execution.

Resource JSON exposes `id`, `projectId`, `name`, `provider`, `environment`, `allowedSchemas` (a JSON array encoded as a string), `active`, `resourceVersion`, `policyVersion` and `createdAt`. It does not expose credentials or `secretRef`. Plan JSON exposes reviewable `ir`, preview SQL and digest; encrypted snapshots and capability hashes remain private. Schema inspection returns `{snapshotId,schemaVersion,resourceVersion,policyVersion,metadata}`.

## Runner API

Every route requires the runner bearer token and an explicit current task attempt.

- `GET /internal/tasks/{taskId}/data/resources?attempt=N`
- `POST /internal/tasks/{taskId}/data/schema` with `{attempt,resourceId}`
- `POST /internal/tasks/{taskId}/data/plans` with `{attempt,resourceId,resourceVersion?,schemaVersion,ir,idempotencyKey}`
- `GET /internal/tasks/{taskId}/data/plans/{planId}?attempt=N`
- `POST /internal/tasks/{taskId}/data/plans/{planId}/explain` with `{attempt,digest}`
- `POST /internal/tasks/{taskId}/data/plans/{planId}/execute-approved` with `{attempt,digest}`

`TaskService` atomically approves the data plan when the user approves a `data_execute` tool request. The internal execution route accepts no user decision or caller-supplied capability. It verifies the persisted approval and creates/consumes its capability inside the control plane. Expired runner attempts cannot adopt newer plans.

## PostgreSQL IR

Metadata includes JDBC column type, nullability, defaults, comments, generated/identity markers, ordered primary keys, foreign keys and indexes. Query IR selects allowlisted columns from one table. Legacy `filters` form an AND list; `where` supports nested AND/OR, scalar comparisons, IN and null tests. The two representations cannot be combined.

```json
{
  "kind": "query",
  "schema": "public",
  "table": "orders",
  "select": ["id", "status"],
  "where": {
    "operator": "or",
    "conditions": [
      {"column": "id", "operator": "in", "value": [1, 2]},
      {"column": "status", "operator": "is_null"}
    ]
  },
  "orderBy": [{"column": "status", "direction": "asc"}],
  "limit": 100,
  "offset": 0,
  "includeRowHashes": true
}
```

`includeRowHashes` requires every table column to be selected. Each hash binds the complete row. A single-row mutation can send its `expectedBeforeHash` to reject changes made after the editor read the row. The hash is SHA-256 of canonical JSON containing a list with that row, not a hash of a rendered cell.

```json
{
  "kind": "mutation",
  "schema": "public",
  "table": "orders",
  "operation": "update",
  "values": {"status": "cancelled"},
  "filters": [{"column": "id", "operator": "eq", "value": 1}],
  "expectedRows": 1,
  "expectedBeforeHash": "replace-with-the-64-character-row-hash"
}
```

Mutation operations are `insert`, `update` and `delete`. An exact `expectedRows` count and a primary key are required; UPDATE/DELETE require a nonempty predicate. INSERT is single-row with `expectedRows=1`. Primary key updates and writes to generated columns are rejected. Bound values are checked against column types: integral ranges, decimal, boolean, date/time, timestamp, text and UUID. Arbitrary unsupported values are rejected.

For browser precision, PostgreSQL NUMERIC/DECIMAL results are decimal strings and BIGINT outside JavaScript's exact integer range is a decimal string. Those strings are accepted back as numeric parameters only for the appropriate numeric column metadata. Keep them as strings in editors. Ordinary safe integers remain JSON numbers; dates, timestamps and UUIDs are strings. Floating point types remain subject to their database precision.

Reads have limits of 1,000 rows and 256 KiB, a ten-second statement timeout and bounded offset 100,000. Primary keys are appended to sorting to break ties. This is stable offset pagination, not a cursor snapshot across concurrent writes. WHERE trees have depth/node bounds; IN has at most 100 values. Raw SQL, JOIN, aggregation, stored procedures and arbitrary functions are not exposed.

## Transactional Migration IR

Migration IR supports at most eight reviewed steps: `create_table`, nullable `add_column` and ordinary `create_index`. Allowed column types are `text`, `integer`, `bigint`, `boolean`, `date`, `timestamp` and `uuid`. Tables created through IR require a bounded primary key. Destructive DDL, arbitrary SQL and concurrent indexes are not accepted.

```json
{
  "kind": "migration",
  "schema": "public",
  "steps": [
    {"operation": "add_column", "table": "orders",
     "column": {"name": "note", "type": "text", "nullable": true}},
    {"operation": "create_index", "table": "orders",
     "name": "ix_orders_status", "columns": ["status"]}
  ]
}
```

All steps run in one PostgreSQL transaction. If a later step fails, earlier changes roll back. A confirmed commit updates the live schema; earlier schema-bound plans fail their drift check. There is no automatically generated reversal of a committed migration. Dependent new-table/new-column steps must use a fresh schema inspection between migration plans.

## Row Recovery

PostgreSQL mutations lock bounded preimages, execute, verify the affected count and collect postimages. Before committing the target transaction, the control plane durably saves an AES-GCM encrypted snapshot containing full before/after rows, hashes, primary keys, schema metadata, resource version and seven-day expiry. Encryption or snapshot-ledger failure rolls back the target. Snapshots are limited to 1,000 rows / 256 KiB.

Committed recovery creates a new PENDING plan and requires a new user approval. It compares current rows by primary key with the complete confirmed postimage and checks schema/resource versions and expiry. Conflicts leave later changes untouched. INSERT recovery deletes the confirmed inserted row; UPDATE restores writable values; DELETE restores original values, explicitly preserving identity keys. Computed columns and triggers are checked against the full preimage before recovery commits, and mismatch rolls back recovery.

若原编程任务已经结束或重试到了其他 attempt，恢复会新建独立 RECOVERY 数据会话，不会重新开启原任务。相同原计划、相同幂等键的并发恢复请求返回同一计划；恢复必须再次批准。恢复失败和数据库事务内失败回滚是两种状态，不能把“提交后补偿”描述为整个系统的全局事务回滚。

## Redis IR And Recovery

Standalone Redis supports namespaced string `get`, `set`, `delete`, `expire` and bounded cursor `scan`. Logical keys map to:

```text
pc:{projectId:resourceId}:environment:logicalKey
```

Thus resources in the same project are also isolated. Reserved `::pcmeta` keys hold permanent monotonic generation metadata; SCAN omits these keys. Metadata persists as a tombstone after deletion and natural TTL expiry. The namespace is version 2; old plans are invalidated and old keys are not automatically copied into v2.

```json
{"kind":"cache","operation":"set","key":"session:1","value":"example","ttlSeconds":120}
```

SET/EXPIRE require TTL 1–86,400 seconds; SET values are limited to 64 KiB. SCAN accepts `cursor` and count hint 1–100, returns `{keys,entries,cursor}` with type, TTL, size and read timestamp per entry. GET returns value, type, TTL, size, hash, generation and read timestamp. A SCAN page is a current view and may have Redis cursor semantics such as duplicate or empty pages.

Writes WATCH both the logical key and metadata, compare the encrypted prepared before-value/hash/expiry/generation, then update value and generation in MULTI/EXEC. Recovery compares the afterimage, metadata hash/write ID and expiry and restores with a new generation. Managed A→B→A changes therefore conflict with stale recovery. A live value without trusted generation metadata is refused; TTL changes or missing metadata also conflict. Use a dedicated managed namespace rather than adopting arbitrary existing keys.

This is a conditional restoration of one key. Redis Cluster, arbitrary data types, raw commands, scripts, `KEYS`, `FLUSH*` and cross-resource keys are not public operations. Direct privileged writers who bypass the gateway can recreate an identical value/TTL without updating metadata; that cannot be proven safe by this generation scheme. Target Redis persistence/replication guarantees depend on its deployment.

## Secrets And Configuration

Registration stores a reference such as `PG_DEV`; the server resolves `DATA_SECRET_PG_DEV`. PostgreSQL secrets contain `url`, `user`, `password`. Redis secrets contain `host`, `port`, optional `database`, `username`, `password`, `tls`. Credentials and the snapshot key are pinned in memory throughout an execution claim and removed afterwards. Pinning does not persist plaintext credentials.

```powershell
$env:DATA_SECRET_PG_DEV = '{"url":"jdbc:postgresql://127.0.0.1:15491/data_test","user":"proofcode","password":"data-test-only"}'
$env:DATA_SECRET_CACHE_DEV = '{"host":"127.0.0.1","port":16491}'
$dataSnapshotBytes = New-Object byte[] 32
[System.Security.Cryptography.RandomNumberGenerator]::Fill($dataSnapshotBytes)
$env:DATA_SNAPSHOT_KEY = [Convert]::ToBase64String($dataSnapshotBytes)
```

Both PostgreSQL mutation snapshots and Redis recovery require the Base64 encoding of a 32-byte AES key. Preserve the production snapshot key across restarts; replacing it makes prior snapshots unreadable. This version has no multi-key decryption or key-rotation worker. Coordinate rotation with outstanding recovery and increment resource versions when changing credentials under an existing reference. Compose services need these environment values passed explicitly. The deterministic all-zero key inside tests is test-only.

## Failure And Audit Semantics

The ledger saves plan creation, user decision, execution start, snapshot preparation and finish. Public result summaries/audits persist counts, hashes, status and bounded error codes, not fetched rows/cache values. Caller-authored IR values are part of the reviewable plan. Private recovery data is encrypted, expires for use after seven days and currently has no cleanup worker.

`COMMIT_UNKNOWN`, `COMPENSATION_UNKNOWN` and `EXECUTING` never automatically replay. A crash after execution start or unavailable finish audit can leave an external change with an unfinished ledger. Reconcile against the provider before proceeding; there is no target-side receipt table in this version. Cancellation/closed sessions prevent a new execution claim but cannot retract an already dispatched provider transaction. PostgreSQL triggers with external effects are outside SQL rollback guarantees; there is no cross-store transaction.

Project isolation is enforced in the API/domain and Redis namespace. Current authentication uses separate global user and runner bearer tokens; this is not team RBAC, per-user project membership or database permission management. Deployment should use dedicated provider credentials and network restrictions; provider accounts define additional permissions.

## Reproducible Verification

The normal data suite covers denied/unapproved plans, explicit task attempts, schema/resource drift, guessed/expired capabilities, duplicate execution, unknown outcomes, redaction, task approval integration, independent session lifetime, terminal-task recovery and concurrent recovery retries. H2 JDBC tests execute parameterized DML, row recovery and snapshot persistence failure rollback. `DataSecretsTest` checks credential/key pinning, nested cleanup and authenticated encryption.

`LiveDataAdapterTest` has nine opt-in scenarios against disposable PostgreSQL and Redis: introspection/EXPLAIN/count rollback; PK/FK/typed paging; durable snapshot failure/row conflict; identity/computed deletion recovery; multi-step migration commit/failure rollback; exact BIGINT/NUMERIC edit and recovery; Redis scans/resource scope/CAS/recovery; managed ABA/tombstones; missing generation/TTL conflicts.

```powershell
docker run -d --name proofcode-data-v13-postgres -e POSTGRES_USER=proofcode -e POSTGRES_PASSWORD=data-test-only -e POSTGRES_DB=data_test -p 127.0.0.1:15491:5432 postgres:16-alpine
docker run -d --name proofcode-data-v13-redis -p 127.0.0.1:16491:6379 redis:7-alpine
docker exec proofcode-data-v13-postgres pg_isready -U proofcode -d data_test
docker exec proofcode-data-v13-redis redis-cli PING
docker compose -f compose.test.yml run --rm -e PROOFCODE_LIVE_DATA_TEST=true -e PROOFCODE_LIVE_DATA_HOST=host.docker.internal control-test mvn -q test
```

The test password, ports and disposable database are fixed in the opt-in test. Use `PROOFCODE_LIVE_DATA_HOST=127.0.0.1` when running Maven directly on the host. Tests use random tables/resource IDs and delete only their own fixtures. Remove the two named disposable containers after verification with `docker rm -f proofcode-data-v13-postgres proofcode-data-v13-redis`. Never point this suite at production resources. These fixture checks establish functional contracts and are not model quality or thesis performance results.
