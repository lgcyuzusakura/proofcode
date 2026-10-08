# 后端会话与受控实验

本文描述控制平面已经实现的工作区/对话作用域，以及 A–F 受控实验 API。它记录执行契约和证据格式，不代表已有真实模型实验结果。

## 工作区与对话

- 工作区属于一个项目，对话属于一个工作区；Task 和 Experiment 均保存 `projectId`、`workspaceId`、`conversationId`。
- `POST /api/projects/{projectId}/workspaces` 创建工作区。`kind` 支持 `REMOTE_REPOSITORY`、`LOCAL_FOLDER`、`SCRATCH`。本接口不接受或持久化任意本机目录路径。
- `GET /api/projects/{projectId}/workspaces` 列出项目的工作区。
- `POST /api/projects/{projectId}/workspaces/{workspaceId}/conversations` 创建对话；对应 GET 路由列出或读取对话。
- `POST /api/tasks` 和 `POST /api/experiments` 可带 `workspaceId`、`conversationId`。控制平面验证整条归属链；跨项目或跨工作区 ID 返回 404。
- 旧客户端不传作用域时，服务在项目锁内取得或创建稳定的 `Legacy` 工作区和对话，避免升级后旧任务接口失效。
- 对话列表 API 是 `GET /api/tasks?projectId={id}&conversationId={id}`。不传 `conversationId` 的旧查询仍返回整个项目的任务。

自动创建桌面工程目录、按目录切换对话的 UI 与本后端 API 分开交付；这些 API 不会在服务器上创建或暴露客户端本机目录。

## 固定实验输入

`POST /api/experiments` 在一个数据库事务内创建实验、所有计划 run、对应 task 和 outbox 记录。重试使用 `Idempotency-Key`；同 key、同请求返回已存在实验，不会重复排队；同 key、不同输入返回 409。

每个实验固定：prompt、模型 ID、项目仓库 URL 快照、完整 Git commit SHA、最大步骤、测试命令、temperature、重复次数和作用域。Commit 必须是 40 或 64 位完整十六进制 SHA；测试命令作为一个直接 executable 与参数交给 Runner，不通过 shell 执行。

实验创建时快照的仓库 URL 会在之后每次重试或审批恢复时继续使用。实验任务的 `branch` 为空，Runner 以 `sourceRevision` fetch 并 detach 到精确 commit。不得改用可变默认分支。找不到固定 commit 会导致该 run 失败并留下失败终结证据。

A 组没有任何 Agent 工具。它可以在最终答案中返回标准统一 diff；Runner 的独立 evaluator 检查并应用 diff，再执行与其他组相同的测试命令。Evaluator 不是暴露给 A 组 Agent 的工具。

六组共享强制执行边界，包括工作区隔离、路径限制、测试命令 allowlist 与数据工具审批；实验不会关闭它们。可变配置如下：

| 组 | 工具 | 确定性安全策略 | Jev | 代码 RAG | 上下文压缩 | 失败反馈检索 |
| --- | --- | --- | --- | --- | --- | --- |
| A | 关闭 | 关闭 | 关闭 | 关闭 | 关闭 | 关闭 |
| B | 开启 | 开启 | 关闭 | 关闭 | 关闭 | 关闭 |
| C | 开启 | 关闭 | 必须启用 | 关闭 | 关闭 | 关闭 |
| D | 开启 | 关闭 | 关闭 | 开启 | 关闭 | 关闭 |
| E | 开启 | 关闭 | 关闭 | 开启 | 开启 | 关闭 |
| F | 开启 | 开启 | 必须启用 | 开启 | 开启 | 开启 |

协议版本为 `proofcode.experiment.v1`。C/F 缺少 Jev 时 run 必须失败，不得静默切换到其他路由策略。Runner 在每条终结事件中报告实际应用的 profile 与源码 commit；比较器只把 `profileApplied=true`、profile version、commit 及可选 group 均匹配的终结结果标为有效。

## 创建示例

```json
{
  "projectId": "PROJECT_UUID",
  "workspaceId": "WORKSPACE_UUID",
  "conversationId": "CONVERSATION_UUID",
  "name": "Controlled comparison",
  "prompt": "修复整数加法溢出问题",
  "model": "固定的 OpenAI-compatible 模型 ID",
  "baseCommit": "0123456789abcdef0123456789abcdef01234567",
  "maxSteps": 15,
  "testCommand": "go test ./...",
  "temperature": 0.25,
  "repetitions": 3
}
```

使用配置好的 bearer token：

```powershell
$headers = @{ Authorization = "Bearer $env:PROOFCODE_AUTH_TOKEN"; "Idempotency-Key" = "thesis-run-001" }
$body = Get-Content -Raw .\experiment.json
Invoke-RestMethod -Method Post -Uri http://localhost:8080/api/experiments -Headers $headers -ContentType 'application/json' -Body $body
```

创建后每组包含 `repetitions` 个实际 Task，并有对应 outbox dispatch。实验对象、run 列表与比较结果可通过以下路由查看：

- `GET /api/experiments?projectId={id}`：按项目列出实验。
- `GET /api/experiments/{experimentId}?projectId={id}`：读取六组比较。
- `GET /api/experiments/{experimentId}/compare?projectId={id}&format=json|csv`：读取 JSON 或下载逐 run CSV。

## 结果证据与统计口径

Runner 通过现有 `task.completed`、`task.failed` 或 `task.cancelled` 终结事件的 `experimentResult` 上报扁平 JSON。例如：

```json
{
  "profileVersion": "proofcode.experiment.v1",
  "profileApplied": true,
  "sourceRevision": "0123456789abcdef0123456789abcdef01234567",
  "experimentGroup": "E",
  "failureReason": null,
  "durationMs": 1234,
  "inputTokens": 1000,
  "outputTokens": 200,
  "toolCalls": 3,
  "toolErrors": 0,
  "testsPassed": true,
  "tokensBeforeCompression": 1200,
  "tokensAfterCompression": 760,
  "compressionTokenBasis": "estimated"
}
```

现有数值指标涵盖时延、用量、工具错误、安全拦截、RAG 排名/命中、压缩 token 数、检索缓存命中、Jev 调用，以及审批前执行次数。布尔指标包括任务/测试/patch 成功、恢复、压缩错误和数据库 SQL、安全、审计、Migration 回滚等。`compressionTokenBasis` 等说明字段与逐 run 证据保存在结果和 CSV 中。Runner 对未实际测量的 SQL/RAG 标签不造默认值；没有证据就是 unknown。

JSON 比较对每组显示 planned/terminal/valid/succeeded/failed/cancelled/configuration-failed/unknown 数。数值指标仅统计有效 run，输出 mean、nearest-rank p95、样本数和缺失数。布尔指标只统计有效 run；`rate` 仅在每个计划 run 均为有效且该指标均已观测时给出，否则是 null。配置不匹配的结果保留在原计划组，并在指标样本数中算缺失。任务状态成功率只在所有计划 run 都终结后给出；失败及未启动组不会被隐藏或推定成功。匹配当前 attempt 的终结事件决定最终状态，避免任务刚完成时两次查询观察到不同快照，把旧 RUNNING 状态与新完成证据混用。

`A–F` 创建 endpoint 本身不声称执行了论文实验。需由用户配置可用模型、Jev（C/F 必需）、Runner、仓库访问和测试环境，运行计划 Task 后再读取比较。测试中的模拟终结事件只验证计算规则，不是论文样本。

## 可复现的工程验证

在安装 Git 和 Go 1.24 或更高版本的环境，从仓库根目录运行：

```powershell
Push-Location .\agent-engine
try {
    go test ./internal/experiment ./internal/agent ./cmd/runner -count=1 -v
} finally {
    Pop-Location
}
```

Runner 的 `experiment_integration_test.go` 启动回环 HTTP 服务模拟控制平面、模型流式响应和 Jev 响应，但实际创建 Git 仓库、通过 HTTP clone、checkout 固定 commit、创建独立 worktree、修改源码并启动 `go test ./...` 子进程。其验证范围包括：

| 测试 | 实际验证的契约 |
| --- | --- |
| `TestSixProfilesActuallyExecuteOnTheSameImmutableInput` | A–F 的开关进入执行路径，模型和 temperature 相同，移动分支不会改变固定源码输入；A 的 diff 经独立 evaluator 应用。 |
| `TestMissingJevDoesNotSilentlyBecomeAnotherGroup` | C 未配置 Jev 时产生显式配置失败，B 仍可独立运行。 |
| `TestFixedEvaluatorRejectsFalseSuccessAndKeepsMissingUsageUnknown` | 模型声明成功不能绕过真实失败测试；缺少 usage 不会变成零 token。 |
| `TestApprovalResumeKeepsProfileAndCollectorState` | 审批前源码未改动；序列化暂停后，恢复保留调用计数、用量和实验 profile。 |
| `TestApprovalResumeCannotChangeExperimentalInputs` | 恢复时修改 prompt 等固定输入会被拒绝。 |

`internal/agent/experiment_test.go` 还覆盖 Jev 不可用、低置信度、未知工具及模型违反工具路由时不得退化，以及一部分请求缺少 usage 后不能再报告完整 token 总计。Collector 的序列化测试验证计数、源码版本和压缩估算口径跨暂停保留。

控制平面的受控任务创建、幂等、当前 attempt 过滤及 JSON/CSV 统计可单独验证：

```powershell
Push-Location .\control-plane
try {
    mvn -s settings.xml -Dtest=ExperimentComparisonSnapshotTest,ExperimentWorkflowIntegrationTest,TaskWorkflowIntegrationTest,TaskEntityLeaseTest test
} finally {
    Pop-Location
}
```

`ExperimentComparisonSnapshotTest` 直接重现旧任务快照与较新终结事件同时被比较器读取的情况，验证成功、失败、取消的状态统计，以及缺失证据和旧 attempt 仍保持 unknown。

这些测试验证工程行为和统计公式。用于论文的数据仍需实际任务集、真实模型响应及标注答案，不能把固定模拟响应的结果当作模型能力或提升比例。

## 服务全链路夹具

`compose.experiments.yml` 使用独立 Compose 项目、网络和命名卷，不发布宿主端口。它启动真实控制平面、PostgreSQL、Redis、Artemis 和 Go Runner；仅模型、Jev 响应及可复现的源码仓库由 `smoke/experiments` 夹具提供。验证请求从公开 API 进入，经过事务 outbox、消息派发、Runner 领取与事件回调，不绕过这些服务直接注入终结结果。

```powershell
docker compose -f compose.experiments.yml up --build -d runner
docker compose -f compose.experiments.yml run --build --rm verify
docker compose -f compose.experiments.yml down --volumes --remove-orphans
```

`verify.py` 验证 A–F 使用六个独立 Task 和同一固定 commit，profile 确实进入执行路径，真实 patch 与共享测试命令通过，D/E/F 的检索证据和 E/F 的压缩指标存在，最后核对比较 JSON/CSV。另一个数据库任务通过真实项目数据网关读取独立 PostgreSQL 中的测试行、生成不可变更新计划并暂停：批准前更新次数必须为 0，用户批准后只更新一次；任务终结后的重复执行被拒绝，拒绝审批的计划不会开始执行。该测试包含模型生成计划、Runner 请求审批、任务恢复和实际数据库写入的完整桥接。

结果与比较导出写入 `.tools/experiment-e2e/`。运行结束后再清理这个夹具的卷；上述 `down` 仅针对 `compose.experiments.yml` 的独立项目。数据库失败回滚和 Redis CAS 补偿由数据网关专项测试验证，不能把本夹具的单次 PostgreSQL 更新描述为覆盖所有回滚情形。

本夹具用于证明工程链路可运行。确定性模型响应没有测量真实模型的决策能力，也没有提供正式 RAG 标注或论文样本。

## 指标的解释边界

- `inputTokens`、`outputTokens`、`totalTokens` 只有全部计入本次运行的模型请求均报告 usage 时才是已知；某次缺失会让总计保持 unknown，恢复后也不会用后续请求补成虚假的完整总计。
- `tokensBeforeCompression`、`tokensAfterCompression` 是各次上下文准备的估算 token 累计，必须与 `compressionTokenBasis=estimated` 一起展示。它们可以描述准备输入的缩减，不能直接等同实际计费 token 的节省。
- `durationMs` 从 Runner 开始处理已领取任务到终结计算，包含仓库准备、工具、测试及审批暂停。它没有单独扣除人工审批等待，比较自动执行性能时应保持审批条件一致并另行记录等待时间。
- `testsPassed=true` 表示固定测试命令确实执行并通过；正式任务完成率还需可靠的验收条件。用于论文的测试集宜由独立 evaluator 保管，避免 Agent 改动测试后影响验收口径。
- Recall@K、MRR、NDCG、正确行号和 SQL IR 准确率需要带答案的数据集。没有标签和测量事件的指标保留 unknown，不能由工具调用次数或模型自述推导。

## 实现边界

- `V10__workspaces_conversations_experiments.sql` 增加作用域、实验和 run 表，并回填旧任务的 Legacy 作用域。
- 计划任务与 outbox 原子提交；重试沿用实验固定输入并使用新的 attempt。比较器忽略旧 attempt 的终结事件。
- 数据库与缓存的连接凭据和安全操作由项目数据网关独立管理；实验框架为这些指标预留字段，不代表数据库/缓存管理 UI 已完成。
- 当前 API/数据模型支持单一全局访问 token 模式，尚未实现团队级用户、角色和细粒度 workspace ACL。
