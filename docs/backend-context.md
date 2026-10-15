# 版本化代码证据与可回取上下文

本文对应 P3 后端的实际实现。生产 Runner 现在使用持久不可变源码清单、Go 语法分块、倒排索引、带权 RRF 与预算覆盖选择、原文回取工具、约束台账和持久压缩视图。没有 embedding、学习式重排或自动模型部署。检索率、低幻觉和速度收益仍需真实任务集测量；这些机制和通过的工程测试不能替代论文性能数据。

## 版本与调用入口

| 标识 | 当前值 |
| --- | --- |
| 实验 profile | `proofcode.experiment.v2-context` |
| 检索算法 | `proofcode.rag.versioned-weighted-rrf.v2` |
| 解析与索引 | `proofcode.index.go-ast-exact.v2` |
| 可回取压缩 | `proofcode.context.retrievable.v2` |
| Token 口径 | UTF-8 JSON 字节除以 4 向上取整，另计 framing/工具定义/预留；仍是估算 |

原 v1 四路文本启发式结果与 v2 结果不能混为相同 D/E 对照配置。Runner 的配置摘要绑定上述版本及模型预算；Collector 在每组报告中保存版本，检索/压缩事件保存具体快照、引用和视图哈希，暂停恢复后保留。

| 文件 | 职责 |
| --- | --- |
| `internal/context/store.go` | 私有 SHA-256 对象、manifest、index、reference、约束台账与模型视图持久化；读取重新核验哈希与作用域。 |
| `internal/context/versioned.go` | 当前捕获、明确历史选择、generation 发布、覆盖与预算选择。 |
| `internal/context/syntax.go` | 有效 Go 的标准库 `go/ast` 分块及静态调用名；其他语言/不完整源码用明确标记的 line-regex 回退。 |
| `internal/context/postings.go` | 不可变倒排词项、符号、引用和测试索引，位置绑定相同 generation 的原文块。 |
| `internal/context/hybrid.go` | 带权 RRF，每一路同一证据只贡献一次，拒绝错误项目/工作区。 |
| `internal/context/durable_compress.go` | 先保存完整原文及约束，再生成预算内模型视图；成功和失败视图均留存。 |
| `internal/context/message_budget.go` | 完整工具协议分组，保护全部 user/system、错误、批准/拒绝和 data 工具记录。 |
| `internal/context/tools.go` | `context_search`、`context_read`，参数不允许模型覆盖权限 scope。 |
| `cmd/runner/experiment.go` | 主/侦察/验证工具注册；主 Agent 每次检索后压缩，错误反馈匹配成功重试后清除。 |

`CompressMessages` 的旧日志去重函数保留给兼容调用与 v1 单元测试，生产 Runner 使用 `CompressDurable`。普通多 Agent 的侦察/验证阶段有原文读取/版本搜索工具；主 Agent 使用检索压缩准备回调。固定 A–F 比较继续使用单 Agent，避免额外模型调用改变对照口径。

## 当前与历史的明确选择

```json
{"query":"OrderService","mode":"current","limit":8}
```

`current` 先记录 HEAD，再完整扫描经过过滤的源码两次。文件路径/字节 manifest 或前后 HEAD 不一致则失败。本轮只使用新核验的单个 generation，不把新字节拼进旧快照。dirty 与允许的未追踪源码包含在真实字节中，删除/改名更新当前路径映射。

```json
{"query":"OrderService","mode":"history","limit":8}
```

`history` 只列最近的持久 generation 元数据：`snapshotId`、`baseCommit`、索引版本、每个文件的路径与字节 SHA-256；不返回混合历史代码。底层 `MaxSnapshots` 默认 4、最大 16。这里保存的是已被系统捕获的版本，并非已经导入仓库全部 Git 历史。源码历史属于 project + workspace，可跨对话/task/attempt 复用。

```json
{"query":"OrderService","mode":"snapshot","snapshotId":"<64位SHA256>","limit":8}
```

选择一个明确快照后只读取它的持久原文。可附完整 `revision` 进一步核验所属 HEAD；只传旧 commit 不等于自动读取任意 Git 历史。快照、原对象或索引损坏时失败，不用当前文件偷偷补齐。

manifest 为规范 JSON 的 SHA-256，绑定源码 scope、HEAD、索引与算法版本、分块参数、文件 BlobHash/原字节块，以及 `IndexHash`。generation 的倒排索引先落盘，再原子发布 manifest 和当前指针；查询只使用该 manifest 的块和 postings。改变 ROOT 但复用相同 project/workspace 与相同源字节时可复用源码 generation；私有工具历史没有这种跨任务权限。

每条 Evidence 带项目/工作区、snapshot、路径/行号/字节范围、整文件 BlobHash、块 SourceHash、ReferenceID、实际 parser、关系依据与命中路由。CRLF 和 Unicode 原字节保持；行号是展示坐标，哈希与字节范围才是来源依据。

当前验证不是跨进程文件系统事务。外部程序可能在最后检查后写文件。`ValidateCurrent` 拒绝已改变的文件，调用方需要持有工作区写锁直到修改结束；现有 apply_patch 自身的上下文/CAS 检查也应保持。历史原文可读不意味着可将旧版直接用于当前 patch。

## 分层检索与增量索引

有效 Go 源码通过 `go/parser` 和 `go/ast` 读取声明、注释、导入与 CallExpr。声明按原字节边界组成块，过长声明再按 80 行的上限拆分，保留声明级符号/调用元数据。没有类型推导、跨包解析、动态 dispatch 或 Go 之外的语法树；其他语言和解析失败的 Go 明确标记 `parser=line-regex`，不宣称是 AST。

| 路由 | 当前评分/依据 |
| --- | --- |
| lexical | 稀疏倒排词项命中，逆文档频率式评分及路径加分；不是完整 BM25，也没有中文专用分词。 |
| symbol | 语法声明名/明确回退正则名的完整与拆分词命中。 |
| dependency | 最多 8 个符号/词法种子及查询词，在 Go 静态调用名/导入词或其他语言文本引用 postings 上扩展。 |
| test | 结合引用匹配、test/spec 路径和同文件词干，不能覆盖所有测试组织方式。 |

融合：`Σ weight(route) / (60 + rank)`。权重 lexical=1、symbol=1.5、dependency=0.75、test=1.2；同一路重复证据不重复加分。融合后以新文件/查询词覆盖提供小幅排序加分，选择整块直到结果数量或字节预算耗尽，不截断源码块。版本与项目硬过滤先于检索评分。

文件解析缓存键含源码 scope、path、BlobHash、分块参数和 IndexVersion。重启从当前持久 manifest 恢复已解析块；未变文件复用，变更文件重新解析。postings 由整个不可变 generation 生成，未改源码查询复用已落盘 postings；它尚不是逐条 postings 的事务增量更新或 watcher 服务。

查询缓存键含算法/索引 generation、query、未解决反馈、路由、结果上限和证据预算。缓存最多 32 个查询键，返回复制避免调用者修改污染。**即便热缓存仍读取并扫描当前工作树两次**；当前主要减少重复解析、词项重建和候选排序，不能宣传 O(1) 热查询。后续 watcher 只能提供提示，不能免除必要来源核验。

## 可回取原文与项目隔离

Runner 私有对象目录默认 `WORKSPACE_ROOT/.context-store`，与任务公开产物和 Git worktree 分开。SHA-256 文件名、0600 文件/0700 目录、原子临时文件替换、长度限制和符号链接拒绝共同保护存储；部署时 Windows 还应确保应用私有目录 ACL。这不是加密存储或抵御已控制宿主账号的攻击者。

源码 manifest/reference 使用 project + workspace；完整工具对象、原 transcript、约束引用使用 project + workspace + conversation + task + attempt。`context_read` 根据可信执行宿主固定的 scope 重新核验，不从模型参数接受 projectId。另一个项目/工作区被拒绝；私有工具日志不能被其他对话、task 或 attempt 读取。

```json
{"referenceId":"<64位SHA256>","offset":0,"maxBytes":65536}
```

引用绑定原对象哈希和允许的起止字节范围。offset 相对该范围，必须非负且不超过长度；maxBytes 为 1–65536。返回 `nextOffset`、`totalBytes`、`eof` 供分页。每次读取重新核验整对象和范围 SHA-256；猜到其他项目 hash 不赋予读取资格。引用和原对象跨重启保持可读。

`bytesBase64` 始终保留分页的准确字节。若分段刚好切断 Unicode 字符，`validUtf8=false` 且 `content` 为空，避免 JSON 编码悄悄替换原文；扩大范围或按 base64 拼接可取得准确数据。正常 UTF-8 段同时提供 `content`。

## 约束保护与模型视图

每次准备输入先保存完整原 transcript、每条原工具 JSON 对象，以及全部 system/user 原文引用。ConstraintLedger 记录来源、下标、role、hash 与 active=true；本阶段保守保留全部用户要求，模型不能自行宣布旧约束失效，不能通过摘要凭空批准操作。

完全相同的成功纯命令日志才会去重，最近一次完整副本保留，旧副本改成含 `contextReferenceId` 的 JSON 导航 envelope。工具调用及结果仍成组、ID/参数不改，不伪装成重新执行的输出。错误、超时、截断、read_file 源码、SQL/JSON/代码、未知格式和短日志不走日志去重。整对象 metadata 不同也不会去重。

预算不足时可以省略完整的旧成功协议组或低价值 assistant 文字，不能只删除一个 tool result。发生省略时，模型视图会加入受保护的完整 transcript 回取入口，并重新核验同一预算。全部 system/user、所有错误/未知结果、批准/拒绝/待执行记录、data 调用组和最新完整组强保护。保护项超预算时直接失败并保存失败视图；不会只留下最新用户而静默忘掉早期要求。

每次压缩持久记录 SourceHash、TranscriptReferenceID、LedgerHash、ViewHash、保护/省略消息下标、替换引用/原哈希、估算前后规模和失败原因。原始 transcript 始终保留；短模型视图可能降低可见信息，报告 `lossless=false`，不宣称模型理解无损。

配置 `PROOFCODE_MODEL_CONTEXT_TOKENS` 和 `PROOFCODE_OUTPUT_RESERVE_TOKENS` 可缩小模型窗口。默认总窗口 200000、输出预留至少16384，加工具 schema 和 framing 后再预算。两项均不能超过硬上限200000，输出预留不得小于实际请求的 max output 16384；无效配置失败。仍未接入真实 tokenizer，提供方 usage 独立记录，不能把估算节省写成真实计费节省。

## 反馈与 A–F 对照

F 的反馈以原工具调用名及规范化 JSON 参数为键记录未解决失败。相同操作的明确成功重试清除该失败；不同命令成功不清除它。选择最近仍未解决的错误末尾最多6000个 rune参与词项查询；无学习式查询重写或在线训练。

D/E 每步重新核验当前源字节，使用固定原任务查询；E 加持久可回取压缩。F 加附加确定性策略、必需 Jev、失败反馈和项目数据工具。全部组共享路径、命令、作用域、批准底线和保护预算。A 不暴露工具；不因实验把数据库批准关闭。

六组工程 fixture 会执行真实 Git worktree、补丁和统一测试命令，模型与 Jev 使用回环模拟服务。它证明对照路径和工程契约，不能作为真实模型的任务成功率、RAG Recall@K、NDCG、幻觉或耗时优势证据。专项真实模型对比仍需固定任务标签、输入预算、正确文件/版本/行号、独立 evaluator 与冷热缓存口径。

## 验证

```powershell
Push-Location .\agent-engine
try {
    go test ./... -count=1
} finally {
    Pop-Location
}
```

新增测试覆盖同文件 dirty 多版本明确选版、源码跨回合复用/私有工具严格隔离、manifest/postings/原对象损坏拒绝、重启原文读取/增量解析、Go AST 原始 CRLF 边界与调用引用、非Go回退标记、原文分页/越界/Unicode字节、工具参数 scope 注入拒绝、协议完整、所有用户约束/失败/批准保护和超预算失败、硬Token环境上限、匹配成功重试清除反馈、Runner注册工具、Collector序列化恢复算法与视图证据。

当前没有源历史过期/垃圾回收管理页面、embedding、多语言 Tree-sitter、完整仓库调用图或跨Runner共享索引服务。原文和索引属于私有应用数据，应配置保留/备份策略，禁止放入公开仓库、公开论文附件或普通任务日志。
