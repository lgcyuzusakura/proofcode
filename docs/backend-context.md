# 工作树检索与上下文压缩

本文对应当前 Go Runner 的实际实现。已实现的能力是四路确定性代码检索、带项目和版本作用域的原文证据、进程内增量解析缓存，以及重复成功日志去重。没有接入 embedding、向量数据库、AST 解析器或学习式重排；检索率、任务成功率和速度提升需通过任务集测量，不能把这些工程机制描述为已经证实的独创算法。

## 执行链路与入口

| 代码入口 | 职责 |
| --- | --- |
| `agent-engine/cmd/runner/experiment.go` 的 `execution`、`experimentView` | 应用实验 profile，给每次主 Agent 模型请求准备检索和压缩视图。 |
| `agent-engine/internal/context/workspace.go` 的 `WorkspaceRetriever.Retrieve` | 读取工作树、校验快照、增量分块、构造四条检索路径、生成证据与缓存结果。 |
| `agent-engine/internal/context/hybrid.go` 的 `Hybrid.Search` | 过滤错误作用域的证据，以 reciprocal rank fusion（RRF）合并排名。 |
| `agent-engine/internal/context/compress.go` 的 `CompressMessages` | 在模型视图中替换完全重复的成功纯日志，保留原始 transcript。 |
| `agent-engine/internal/agent/agent.go` 的 `Agent.Run` | 克隆原始消息，调用 `PrepareMessages`，再检查模型输入预算并发出请求。 |
| `agent-engine/internal/agent/context_budget.go` | 按完整工具调用组裁剪过大模型输入；校验工具调用与结果的对应关系。 |
| `agent-engine/cmd/runner/main.go` 的 `runTask`、`workspaceMetadata` | 持久化原始消息、usage、审批状态和实验 Collector；暂停恢复继续使用原始记录。 |
| `agent-engine/internal/experiment/collector.go` | 从真实事件收集检索次数、缓存命中、压缩估算及快照来源。 |

主 Agent 每次请求的大致流程为：

```text
原始 transcript 的副本
    → 若开启压缩：去重重复成功日志，发出 compression 事件
    → 若开启 RAG：校验当前工作树并检索，注入本次 repository evidence
    → 按模型输入预算选取完整消息组
    → 工具路由与模型请求
    → 执行工具，将完整结果追加到原始 transcript
    → checkpoint / 审批暂停时持久化原始记录
```

当前 `PrepareMessages` 接在主 Agent 请求上。普通任务还可能启用 scout/verifier；它们没有复用这条检索压缩回调。六组实验运行单 Agent，避免因额外子 Agent 调用改变比较口径。

## 四路混合检索

工作树按原始 UTF-8 字节读取，以固定行数分块，默认每块 80 行；CRLF、原始内容和字节坐标均保留。分块是确定性行块，不保证恰好覆盖一个完整函数。

| 路径 | 当前规则 | 能力限制 |
| --- | --- | --- |
| `lexical` | 对查询与路径/源码分词，拆分部分 camelCase 和下划线标识符；按词项命中的逆文档频率式权重排序，路径命中额外加分。 | 是词项存在性启发式评分，没有 BM25 的长度归一化或完整词频建模；中文没有专用分词器。 |
| `symbol` | 正则提取 `func`、`def`、`class`、`interface`、`type`、`fn` 等声明名；匹配完整名及拆分词。 | 没有语言级语法树、重载解析或类型推导。 |
| `dependency` | 取符号/词法排名的前若干种子，移除声明匹配片段后查找声明名、查询词与文件名词干的引用。 | 是文本引用线索；注释和字符串可能产生误命中，不等于调用图或 import 图。 |
| `test` | 查找路径含 `test`/`spec` 的块，结合种子文件词干、引用与查询词排序。 | 文件名规则不能覆盖所有测试组织方式。 |

关系检索最多取合并种子中的前 8 个命中，优先来自符号路径。四路各给出排名，融合不直接相加不同路径的原始评分：

```text
RRF(chunk) = Σ 1 / (60 + rank)
```

这里 `rank` 从 1 开始；`Hybrid.RankK` 可在代码层配置，默认 60。同一证据被更多路径命中时累积贡献，并记录 `Reasons`。相同得分依次按路径、起始行、证据 ID 排序，保证可重复。Runner 请求最多 12 个融合结果；每条路径先取最多 36 个候选。最终仍受证据字节预算限制，超预算整块省略，不切割块内源码。

`Hybrid` 接口允许将来增加检索器，但当前生产 `WorkspaceRetriever` 只构造上述四条路径。测试里名为 `vector` 的静态假检索器只验证融合公式，不能当作已接入向量检索的证据。

## 项目、版本和快照隔离

每次检索先读取 Git HEAD，再完整扫描候选源码两次。若两次文件 manifest 或前后的 HEAD 不一致，返回 `working tree changed during snapshot capture`，本次不注入混杂版本证据。这个校验能发现两次扫描之间的变化；它不是文件系统事务锁，最后一次检查后的外部写入仍需由下一次检索重新识别。

快照的作用域包括：

```text
indexVersion = worktree-evidence-v1
scope = indexVersion + absoluteRoot + projectId + workspaceId
        + taskId + attemptId + gitHEAD
manifest = sorted(relativePath + SHA256(fileBytes))
snapshotId = SHA256(scope + manifest)
```

实际连接使用 NUL 分隔，文件 manifest 按路径排序。没有传 project/workspace 时，底层检索器根据根目录生成默认哈希作用域；Runner 传入真实 project/workspace/task/attempt。只有通过扫描过滤的文件进入 manifest，因此忽略文件的变化不直接改变它；HEAD 改变仍会生成新快照。

每段 `Evidence` 包含：

| 字段 | 含义 |
| --- | --- |
| `projectId`、`workspaceId`、`snapshotId` | 证据归属与精确快照；task/attempt 已参与 snapshot 哈希。 |
| `path`、`startLine`、`endLine` | 原文件位置与用于展示的行坐标。 |
| `blobHash` | 整个文件原始字节的 SHA-256；字段名不代表 Git blob object ID。 |
| `sourceHash` | 当前块原始字节的 SHA-256。 |
| `startByte`、`endByte` | 文件中的零起点、左闭右开字节范围，可检查 `fileBytes[startByte:endByte] == content`。 |
| `id` | 由快照、路径、字节范围和块哈希生成的证据标识。 |
| `content`、`score`、`reasons` | 完整原文、RRF 分数与实际命中路径。 |

融合前要求项目、工作区和快照均匹配。去重键包含这些作用域和证据 ID，同一行号、同一符号在不同版本中不会被当作同一证据。

当前模型是“当前工作树的精确版本证据”。它没有长期保存同一文件的所有历史版本，也没有实现让用户在一个历史向量库里选择旧版本。实验通过完整 commit SHA 固定初始源码；要检索其他历史版本，需要另一个对应 revision 的工作树或未来的历史索引。

## 修改源码后的证据更新

D/E/F 每次主 Agent 请求都调用 `Retrieve`，包含 patch 之后的下一次请求。已有 `initial` 变量不会让后续请求固定使用首次证据：D/E 同样重新验证实际字节，只是不加入错误反馈来调整查询。

假设 `math.go` 从版本 v1 变为 v2：

1. 文件内容哈希和 manifest 改变，生成新的 `snapshotId`。
2. v2 的分块与证据 ID 重新生成；未改动文件可复用已解析块，但重新绑定到新快照。
3. 旧快照的查询结果从缓存移除；删除的文件从当前解析缓存移除。
4. 当前请求注入新的完整证据，不把 v1 的旧字节/行号当作 v2 原文。

注入内容包含 snapshot/blob/行/字节坐标，并提示模型把证据作为源码数据、编辑前读取当前文件。这个提示提供来源线索，不保证模型不幻觉，也不代表用户已批准任何操作。

## 缓存键、增量复用与成本

缓存位于 `WorkspaceRetriever` 实例的内存中，由 mutex 串行保护；它不是项目的 Redis 缓存，也不是跨 Runner 共享服务。审批恢复或进程重启创建新实例时可重新构建，原始 transcript 和实验计数仍从持久化状态恢复。

| 缓存层 | 键 | 命中行为 |
| --- | --- | --- |
| 文件解析缓存 | `scope + relativePath + fileContentSHA256 + chunkLines` | 复用未变化文件的精确分块；变更文件重分块，删除文件和旧 scope 被清理。 |
| 查询结果缓存 | `snapshotId + query + feedback + resultLimit + byteBudget + chunkLines` | 复用当前快照下相同查询的融合结果，返回深度复制的 slices，避免调用者修改缓存证据。 |

切换 HEAD、项目、工作区、task 或 attempt 会改变 scope；即使文件字节一样，也不会跨这些作用域复用原解析条目。仅工作树中的部分文件变化时，未变文件才复用解析。

结果缓存只保留当前快照，最多 32 个查询键；达到上限后清空，再放入当前结果，它没有 LRU 或持久化淘汰策略。`CacheHit=true` 表示查询结果复用；`FilesParsed`/`FilesReused` 表示解析复用，不代表磁盘读取被省略。

为检测变化，即使命中查询缓存，每次仍扫描并读取工作树两次。缓存主要省掉分块、路由评分和融合工作，不能宣称 O(1) 查询或完全消除大仓库扫描延迟。不同 query/feedback 的词法与引用评分当前仍遍历候选块。

## 压缩规则与原始记录

`CompressMessages` 克隆消息和工具调用参数，生成模型专用视图。Agent 将工具完整结果追加到原始 `messages`，checkpoint、暂停和恢复持久化使用这些原始消息；检索注入和压缩替换不会覆盖它们。

目前只对满足全部条件的工具输出去重：

- 工具结果可解析为 JSON，`isError` 未标为 true；metadata 明确包含 `exitCode=0`。
- 没有 timeout、截断等异常标记，日志中不含 `failed` 或 `error:`。
- 内容每个非空行符合支持的纯日志前缀，例如 `INFO`、`DEBUG`、`PASS`、部分 Go test 输出；包含 JSON、SQL、代码、命令或结构字符等线索时保留。
- **整条序列化工具结果字节完全相同**，不只是内层日志文字相似。
- 替换引用比原文更短，否则保持原文。

它从新消息向旧消息扫描，保留最近一次完整成功日志，将早期重复副本替换为含 `sha256` 和 `sourceHash` 的短引用。消息数量、role、toolCallId、工具名称和参数保持对应，不用生成式模型重新编写日志。

错误日志、超时或截断日志、read_file 源码、任意 JSON/SQL/代码结果、未知工具格式和短日志都保留。匹配规则较保守，并不是所有成功命令都能压缩；例如 metadata 的工作耗时变化，也会导致整条结果哈希不同而无法去重。不能承诺任意长对话都有显著缩减。

报告包含：

| 字段 | 用途 |
| --- | --- |
| `sourceHash` | 原始消息序列 JSON 的 SHA-256。 |
| `sourceReferences` | 原消息下标、toolCallId 和结果哈希组成的来源引用。 |
| `deduplicatedMessages` | 真正被短引用替换的消息数。 |
| `beforeEstimatedTokens`、`afterEstimatedTokens` | 当前调用前后估算输入规模。 |
| `lossless` | 当前去重保持一份完整副本及原始记录；不表示后续输入裁剪或模型理解完全无损。 |

来源引用是验证标识，当前没有自动按该引用读取原日志的独立 API。`compressedMessages` 字段存在，但当前实现不做摘要式改写，该计数保持 0。

## 估算与容量限制

Token 估算按 UTF-8 **字节长度除以 4 向上取整**。它不是模型 tokenizer，中文、代码、长标识符和不同模型可能偏差明显；压缩报告也不完整计入 API 消息封装等开销。实际计费用量以提供方 usage 为准，不能将估算减少直接宣称为实际 token 节省。

| 限制 | 当前默认值 / 行为 |
| --- | --- |
| 候选源码数量 | 最多 4,000 个文件；超限返回错误。 |
| 候选源码总大小 | 最多 32 MiB；超限返回错误。 |
| 单文件大小 | 最多 512 KiB；符合源码名规则的过大文件导致检索失败，不静默忽略。 |
| query + feedback | 最多 128 KiB，按字节判断。 |
| 分块 | 每块 80 行，代码层可配置。 |
| 检索返回数量 | 底层默认 8，上限 64；Runner 当前传 12。 |
| 注入证据预算 | 48 KiB，包含证据标题；超预算整块省略，记录 `OmittedByBudget`。 |
| 查询缓存 | 当前快照最多 32 个键，达到上限清空。 |
| 失败反馈 | 最近一条可解析的失败工具结果，最多保留末尾 6,000 个 rune。 |
| 模型输入预算 | 常量 `200000 - 16384 = 183616` 个估算 token，输出请求上限 16,384。 |

检索器容量可通过 Go struct 字段配置；Runner 当前使用上述默认值，没有专门的每项目 UI。源码过滤跳过常见依赖/构建/元数据目录、敏感文件名、符号链接、含 NUL 的二进制、无效 UTF-8，以及匹配的私钥/敏感赋值。检测是启发式规则，不是完整敏感信息识别；它也没有按 `.gitignore` 建立 Git 索引，所以其他符合条件的未追踪文件会进入当前工作树证据。

模型输入预算检查与日志去重是不同机制。预算检查保留 system 消息和最新用户请求，按完整的 assistant 工具调用及对应 tool 结果分组，优先移除更早会话，再移除最新请求后的较早工具组；不会只留半个工具协议。基础 system、最新用户请求和工具定义仍超预算时直接报错。它会损失本次模型视图中的历史内容，不能把它描述成无损压缩；原始 transcript 仍保留。

这些预算是应用当前常量，并未根据每个模型的真实 context window 自动调整。不同提供方可能在应用预算之前拒绝请求，需要将来接入模型能力配置和真实 tokenizer。

## D/E/F 的实际差异

| 组 | 混合检索 | 重复成功日志去重 | 失败反馈调整检索 | 其他开关 |
| --- | --- | --- | --- | --- |
| D | 开启；每步以原 task prompt 检索当前字节 | 关闭 | 关闭 | 工具开启；确定性附加策略和 Jev 路由关闭。 |
| E | 与 D 相同，每步重新校验工作树 | 开启 | 关闭 | 与 D 相同。 |
| F | 开启；每步将最近失败工具结果追加到查询 | 开启 | 开启 | 确定性附加策略和必需 Jev 路由开启，提供项目数据工具。 |

所有组保留强制路径、工作区、命令和数据审批边界，通用模型输入预算检查也适用于全部组。E 相对 D 的独立变化是去重；F 同时变化多个功能，不能把 F 的全部差异都归因于压缩或检索。

`latestFailure` 从完整消息中向前寻找最近失败；之后一次成功结果不会自动清除先前失败文本。当前反馈是“把失败文本用于下一次词项查询”，没有另一次 LLM 查询重写、在线训练或自动评估迭代率。

每次 `context.selected` 事件记录检索快照、HEAD、缓存命中、解析/复用数量、证据引用、路由、`embeddingConfigured=false` 和是否用了反馈。压缩事件记录估算与来源哈希。Collector 汇总 `retrievalCalls`、`retrievalCacheHits`、`deduplicatedMessages` 和压缩估算累计，并标记 `compressionTokenBasis=estimated`；这些是工程运行证据，不直接代表 Recall@K 或 NDCG。

## 验证范围与复现

在安装 Git 和 Go 1.24 或更高版本的环境，从仓库根目录运行：

```powershell
Push-Location .\agent-engine
try {
    go test ./internal/context ./internal/agent ./internal/experiment ./cmd/runner -count=1 -v
} finally {
    Pop-Location
}
```

| 测试文件 | 覆盖内容 |
| --- | --- |
| `internal/context/workspace_test.go` | CRLF 原字节坐标、同文件内容版本更新、项目/工作区/commit/task-attempt 隔离、反馈四路命中、仅重解析变化文件、删除证据清理、返回 slices 不污染缓存、敏感/二进制/链接过滤、容量与取消、预算整块省略。 |
| `internal/context/hybrid_test.go` | 多条路径一致命中时 RRF 排序提升；静态假检索器不代表真实向量服务。 |
| `internal/context/compress_test.go` | 原始 transcript 不变、最新完整成功日志保留、工具参数不别名、失败/超时/截断/JSON/SQL/代码保护、短日志不膨胀、无法序列化的记录不生成不可验证替换。 |
| `internal/agent/context_budget_test.go` | 裁剪完整旧工具组、保留当前请求、超大当前请求报错、孤立工具结果拒绝。 |
| `internal/agent/experiment_test.go` | 模型视图不修改原始记录，审批序列化恢复保留步骤和 usage；其他实验路由/用量边界。 |
| `internal/experiment/collector_test.go` | 压缩估算、快照来源与计数跨序列化保留；未持久化事件不计入结果。 |
| `cmd/runner/experiment_integration_test.go` | 六组在相同固定 commit 上实际走对应检索/压缩/路由执行路径，真实 Git worktree、patch 和测试命令；模型与 Jev 响应由回环 HTTP fixture 模拟。 |

缺 Git 的版本测试会跳过；符号链接测试在系统不允许创建链接时只记录该子项未覆盖。工程测试证明来源与执行契约，不能代替真实仓库任务集的检索率、速度、低幻觉或总体能力实验。论文中应使用带正确文件、版本、行号和任务验收的标注集，单独报告缓存冷/热状态、仓库规模和压缩规则的实际命中比例。六组 API 与统计口径见 `docs/backend-experiments.md`。
