# ProofCode 上下文管理升级方案

日期：2026-10-13

2026-10-14 实现进展：逐轮排除/恢复、聚焦模式、手写摘要替换、历史投影预览、任务快照与恢复校验、命名上下文包及新对话带入已实现。范围与测试见[更新报告](update-report-2026-10-14.md)。下文保留原设计，不代表其中所有 P0/P1 能力均已落地。

## 结论

这是一个真实而且有产品差异化的痛点。本次核查没有验证一个同时满足全部要求的成熟 coding 工作台，但局部机制已经存在，不能宣称首创。值得做的是把逐条上下文选择、保留原始记录、跨对话带入和工具执行状态整合进 ProofCode 的日常工作流。

建议把 ProofCode 的核心模型改成：原始聊天是完整档案，模型每次看到的是一个可编辑的上下文投影。用户排除的是“本次模型输入”，不是删除聊天记录。

## 有真实代码或文档依据的参考

- [Pi Sessions](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/sessions.md)：支持 /tree 在会话树内切换，/fork 从较早用户消息创建新会话，/clone 复制当前分支；离开分支时可以把旧分支摘要附加到新分支。
- [Pi Compaction](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/compaction.md)：自动或手动压缩，原始条目保留；支持 branch summary 和 context edit。
- [Pi Session Manager](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/src/core/session-manager.ts)：ContextEditEntry 的 replacement 为 null 时，从模型上下文省略目标条目；也可以用 replacement 替换内容。这是和你说的“这段聊天不再喂给模型”最接近的已实现机制。
- [SillyTavern 消息隐藏实现](https://github.com/SillyTavern/SillyTavern/blob/release/public/scripts/slash-commands.js#L1835)：/hide 按消息编号或区间把聊天消息从 prompt 隐藏，/unhide 恢复；[请求构造源码](https://github.com/SillyTavern/SillyTavern/blob/release/public/script.js#L4496) 有对应过滤。它是聊天前端而非 coding agent，但证明细粒度上下文排除已有实际产品实现。本次未验证其具有我们拟做的逐条勾选面板。
- [trimContext](https://github.com/SnowBeatRain/trimContext)：只读分析、健康报告、保守压缩副本和 new-chat 交接包；new-chat 会生成 handoff.md、next-context.md 和 manifest。它不会改原始 transcript。
- [cczip](https://github.com/unclecode/cczip)：按词汇相关性压缩，保护开头两段范围、结尾三段范围和用户消息边界，支持预览、备份及恢复。README 的 ranges 不能翻译成固定两条、三条消息。
- [LangChain short-term memory](https://docs.langchain.com/oss/python/langchain/short-term-memory)：明确把 trim、delete、summarize、custom filtering 分开；可以在调用模型前重写消息集合。
- [Letta memory](https://docs.letta.com/guides/agents/memory/)：所有消息持久化，即使被压出上下文仍可取回；memory blocks 可以 attach/detach，也能跨 Agent 共享。
- [Open WebUI](https://github.com/open-webui/open-webui)：README 明确列出 Notes、Persistent Memory，以及向聊天注入 Notes/文档上下文的能力。
- [cogpit-memory](https://github.com/gentritbiba/cogpit/tree/main/packages/cogpit-memory)：规范化 Claude Code、Codex 和 Copilot CLI 历史，并提供跨会话全文搜索。
- [contextprune](https://pypi.org/project/contextprune/)：项目定位是 LLM context window garbage collection。
- [llm-sieve](https://github.com/llmsieve/llm-sieve)：README 描述通过代理抽取持久事实、存入本地加密库并按需注入；未独立验证其宣传的节省比例。
- [Tura 上下文文档](https://github.com/Tura-AI/tura/blob/main/docs/core/context-management.md)及[运行时业务测试](https://github.com/Tura-AI/tura/blob/main/crates/runtime/tests/business/runtime_context_business_flow.rs)：把原始事件与 provider context 分开，通过 context_compaction 保留目标、证据、工作区状态和下一步；task_status.compact_context 可以生成任务检查点。适合参考结构化交接，未验证任意逐条图形选择。

### 对用户提供的项目清单的核查修正

- trimContext 的 new-chat 生成交接文件供审阅和粘贴，不是自动恢复会话，更不是恢复服务商的模型缓存。README 明确说 Codex 当前窗口绑定尚未验证，不能当作已验证的 Codex 插件流程。
- contextprune、cogpit-memory、llm-sieve 的包或仓库可核实，但分别偏垃圾回收、历史检索、持久事实记忆，不能等同于完整的上下文编辑工作台。
- [context-window-manager 的 PyPI 包](https://pypi.org/project/context-window-manager/)存在；其所列 pranaym/context-window-manager GitHub 地址在本次核查返回 404，因此只确认包页存在，不把其生产能力当作已核实结论。
- Pi 的上下文编辑有源码依据，但不声称它自带逐条勾选的桌面面板。来源事实、README 的主张和我们提出的产品设计应分别表达。

### 这些参考说明了什么

Pi 是“会话树 + 追加式上下文编辑”的最直接参考；Letta 是“持久消息 + 可挂载记忆块”的最直接参考；trimContext 是“新对话交接包”的最直接参考；cczip 是“相关性压缩和保护区”的参考。它们需要组合，才接近你的完整诉求。

## ProofCode 现在的差距

当前代码已经有正确的基础：ConversationMessageService 保存完整聊天；Agent Engine 已有输入预算、工具协议成组保留、Durable Compression、原始对象引用和 context_read；任务也已有 Provider 配置快照。

问题是用户还不能控制模型看到什么：

- ConversationMessage 已有稳定 id、taskId、attempt 和 sequence；runner 的 conversationHistory 只拿 role/content，丢掉了来源身份，也没有消息级上下文状态。
- ConversationMessageService.history 默认把已完成历史全部返回。
- internal/context/message_budget.go 将历史用户消息视为 protected，能防误删，但无法表达“用户明确排除这条独立支线”；durable_compress.go 也将所有用户约束默认记为 Active。
- CompressDurable 已能保存原始记录和压缩引用，却没有 Context Drawer、跨会话 Context Pack 或任务级 context snapshot。
- history 在过滤前超过 1000 条或 2 MiB 就拒绝继续；改造后应分页读取、先构造选中投影，再检查请求预算，同时保留存储和传输上限。
- CHAT 和新 CODE 任务读取 conversationHistory；恢复执行则使用 saved.Messages。只修改一个历史接口，无法覆盖恢复路径。

## 目标产品模型

### 1. 三层数据

- Transcript：不可变的原始聊天、工具调用、审批和结果。
- Context View：某个会话的可编辑模型投影，描述每条消息 INCLUDE、EXCLUDE、PIN 或 REPLACE。
- Context Pack：可跨会话挂载的命名上下文包，例如“支付模块约束”“数据库迁移方案”“登录失败证据”。

### 2. 三种模式

- 自动：保持普通 Agent 体验；先应用用户排除，再让符合条件的历史进入现有预算和压缩流程。
- 聚焦：只发送当前请求、PIN、用户选中的消息和挂载的 Context Pack。
- 混合：发送聚焦上下文，并允许从挂载的 Context Pack 按需检索。

### 3. 消息状态

- INCLUDE：有资格进入投影，仍受预算和压缩约束；界面继续说明实际发送的是原文、摘要还是可检索引用。
- EXCLUDE：从模型输入、工具检索和记忆注入中排除，原始聊天仍显示。它不同于因预算暂时省略但可检索的内容。
- PIN：自动压缩也不能移除；固定内容超预算时明确报错并提示调整，不静默丢弃。
- REPLACE：用用户编辑的短摘要替代原文进入模型；原文保留供用户查看，默认不以引用重新注入。是否允许模型检索原文应作为另一个明确选项。

assistant tool call 和对应 tool result 必须作为一个协议组处理，不能只选一半。当前用户请求、系统规则、正在执行的任务状态和有效审批不属于普通聊天排除操作；旧失败证据默认保护，可通过完整任务组管理。用户排除旧的普通聊天支线时，不改变审计记录，也不注入“请忽略某消息”之类的新文本。

### 4. 摘要来源与污染处理

摘要、记忆和上下文包必须记录 sourceIds、sourceHashes 及派生关系。排除来源后，所有依赖该来源的当前摘要、检索索引投影和请求缓存投影失效；从剩余原始材料重新构造，不能把旧的污染摘要再压缩一遍。

后续回答可能已经复述或依赖被排除话题。系统应标出可识别的关联消息并提供“排除整段支线”，但不能承诺删除一条源消息就消除了所有已生成文本的影响。旧摘要没有来源信息时，标为过期并从原始记录重建，不伪造追溯关系。

用户手写摘要可以直接使用；默认清理和选择不调用模型。可选的模型摘要只读取过滤后的材料，先显示预估开销，再生成并保留来源。

### 5. 跨对话上下文与模型缓存

可以带走的是本地选择的消息、摘要、约束和证据包。服务商的 KV cache / prompt cache 不作为可迁移资产：命中通常依赖实际前缀、模型、服务商、配置和有效期，编辑或换模型可能导致未命中。界面显示服务商实际返回的缓存 Token；未返回就标为未知，不把本地复用估算成缓存命中。

## 前端方案

在聊天顶部增加“上下文”入口，打开 Context Drawer：

当前请求上下文 12.4k / 16k input
[自动] [聚焦] [混合]    [预览请求]
固定 2 条 · 已包含 18 条 · 已排除 7 条

每条消息显示预计 Token 和状态：包含、排除、置顶、替换。批量操作支持按一轮、手选主题区间、从某条到当前；自动主题识别放到后续建议能力。按钮用“从模型上下文排除”，避免用户误以为历史已删除。默认按一整轮选择，展开后查看具体消息和工具组。

侧栏同时显示最终输入清单、原文/摘要/引用占用、过期摘要及来源。修改后先显示“下次任务生效”，不在当前流式运行中偷偷换上下文。用户查看原始聊天与 Agent 读取上下文使用不同入口，避免 UI 审计权限变成模型读取权限。

现有聊天区的滚动、跳到最新、搜索和消息定位是基础体验，必须保留；上下文面板不能挤掉聊天滚动容器。桌面用可调整宽度的右侧栏，小屏用抽屉，长列表采用分页或虚拟列表。

底部提供三个动作：

- 保存为上下文包：命名并保存选中消息、用户摘要、工具结果引用和文件快照引用。
- 从这里分叉：保留共同祖先，创建一个新的 conversation branch；不隐含回滚 Git 或工作区文件。
- 新对话并带入：创建新 conversation，挂载选中的 Context Pack，并显示来源版本和消息数。

新对话顶部应显示“来自：修复登录 / 上下文包 v3 / 8 条消息”，并允许一键卸载。

## 后端数据和接口

建议新增以下实体（具体表名沿用仓库命名约定）：

- conversation_context_views / context_view_versions：会话和视图身份、parent_view_id、mode；每次操作生成不可变版本及追加操作记录，current_version 指向当前版本。
- conversation_context_items：view_version_id、message_id、state、replacement_text、source_hash、selected_spans、derived_source_ids、protocol_group_id。不依赖易变化的消息序号识别来源。
- context_packs / context_pack_versions：包身份和不可变版本分开；manifest 保存来源 scope、消息及 task/attempt、内容 hash、选中范围、摘要方法/版本、可选生成模型、依赖来源与文件 revision。
- conversation_context_mounts：目标会话、pack_version_id、授权范围、启用状态和挂载顺序；源视图排除发生变化时标记依赖过期，不继续悄悄注入旧副本。
- task_context_snapshots：task_id、attempt、view_version_id、挂载包版本、manifest、selection_hash、provider configuration hash、created_at。

原始 conversation_messages 不增加删除语义。任务启动时锁定不可变 ContextSnapshot；对同一逻辑任务的重试沿用选择，新的 attempt 记录关联同一 selection_hash。用户修改视图后只影响后续任务；如需立即采用，应停止当前任务并创建新任务。恢复检查点必须校验快照标识，不能用 saved.Messages 绕过选择。

context/store.go 的 Read 当前校验 project/workspace/conversation/task/attempt scope。跨对话带入必须把获准的选中材料重新登记到目标 scope，记录来源及 pack version；不能直接复制旧 referenceId，也不能放宽整个存储的 scope 校验。挂载前校验目标项目/工作区权限，文件和测试证据应注明旧版本并在新任务核对现状。

建议接口：

- GET /api/.../conversations/{id}/context
- PATCH /api/.../context/items/{messageId}
- POST /api/.../context/views
- POST /api/.../context/fork
- POST /api/context-packs
- GET /api/context-packs
- POST /api/.../conversations/with-context
- POST /api/tasks/{task}/context-preview

context-preview 必须与正式请求共用同一投影/预算构造逻辑，是 dry-run：返回实际 provider 格式的输入预览、消息清单、预计 Token、排除原因、引用和压缩前后差异，不调用模型、不产生任务副作用。Token 标明估算方法；服务商实际计费 Token 以请求返回为准。预览绑定视图、包、配置版本，发送前版本变化时重新计算。

## Agent Engine 改造

把当前 CompressDurable 拆成三个确定步骤：

1. 应用用户投影：EXCLUDE 不进入输入及 Agent 可检索集合，REPLACE 使用 replacement，PIN 加入 protected 集合。派生摘要和约束 ledger 的 Active 状态也由投影决定。历史用户消息不能仅因 role=user 就永久 protected；区分系统规则、当前请求、有效审批、明确固定的约束与普通历史讨论，避免重现“所有旧话题都是永久约束”。
2. 应用预算压缩：只对有资格的项执行已有的工具协议成组裁剪、去重和 durable reference；引用仅指向本视图允许的内容。完整原始 transcript 继续作为审计档案，不把其全集 referenceId 暴露给 Agent。
3. 输出 ContextReport：区分原始 Token、用户排除 Token、自动压缩 Token、PIN 项、引用 ID 和允许的恢复路径；附 selection_hash 与摘要来源。

自动相关性排序只提供建议，不能静默改变 INCLUDE 或 PIN。原始记录永远保留；所有投影变更追加记录，便于撤销和审计。

普通 CHAT 与 CODE 共用上下文构造器；恢复 CODE 使用启动时的快照和该任务自己的运行增量，而非当前视图重读历史。批准、工具调用状态和已发生的执行副作用独立保存，不通过删聊天来撤销。

实现位置：Control Plane 的 ConversationMessageService / 任务创建负责版本和快照；Go runner/chat.go、main.go 负责解析稳定来源与恢复校验；internal/context/message_budget.go、durable_compress.go、store.go 负责投影、预算与过滤后的引用；React 的 MessageActions、Transcript、RunResources 增加上下文操作、来源定位和报告。借鉴机制，用现有 Java/Go/React 架构实现，不需要为了功能安装这些项目的 CLI。

## 分阶段实施

### P0：先解决你现在的痛点

1. 保留已有消息 id、taskId、attempt、sequence 到 runner，新增视图版本及 context state；按完整问答轮/协议组选择。
2. 做 Context Drawer，支持逐轮排除、恢复、置顶和 Token 预览。
3. 创建 task_context_snapshots。
4. CHAT、CODE 及检查点恢复都遵循快照；legacy 无快照按兼容自动模式启动，但不允许绕过已有排除。
5. 做版本固定的 Context Pack，并支持新对话带入、来源显示及卸载。
6. 加入摘要来源失效、过滤后的 context_read 与干净摘要重建。该项是 P0 正确性要求，不留到智能优化阶段。

### P1：形成明显差异

1. 会话树和从任意用户消息分叉。
2. Context Pack 版本、搜索、挂载和卸载。
3. 压缩 dry-run 对比：用户排除、自动压缩、当前预算分开显示。
4. 一键生成 handoff：目标、约束、进度、未完成、关键文件和来源引用；模板整理无需模型，选择模型生成时显示开销。

### P2：智能建议

1. 标记可能与当前主题无关的旧消息，但默认不执行。
2. 对已解决错误、重复工具输出和过期讨论给出清理建议。
3. 从用户挂载的 Context Pack 做可解释检索。
4. 工作区级默认上下文模式和保护区。

## 验收标准

- 排除一段无关聊天后，下一次 provider request、context_read、自动记忆注入均不包含它；聊天和审查仍可查看。不追加“忽略”指令。
- 排除已进入摘要的来源，旧摘要失效；从剩余原文重建。无来源的 legacy 摘要不继续沿用。
- 排除 assistant tool call 时，关联 tool result 一起排除，绝不产生半个工具协议。
- 当前请求、有效审批、受保护失败证据和 PIN 内容不会被自动压缩删除；固定内容超过输入预算时可解释地拒绝发送。
- 新对话带入 Context Pack 后只携带包中内容，来源、版本和消息数可见。
- 同一任务重试使用相同 selection_hash。
- CHAT 与 CODE 的选择一致；修改视图不改变已运行任务，saved.Messages 恢复不能重新引入已排除历史。
- 跨会话只赋予选中内容的目标 scope，原始会话其他内容及旧引用仍不可访问。包来源过期时提示刷新，不静默注入旧污染摘要。
- 对话分叉不会暗中回滚工作区，新任务重新验证文件版本和旧测试结论。
- dry-run 不调用模型、不修改 transcript；确认后可恢复上一版 view。
- 自动模式行为兼容。
- 浏览器测试覆盖逐轮排除、批量区间、恢复、分叉、新对话带入、移动端 Drawer 和请求预览。

## 首版推荐范围

用户入口直接叫“上下文”，操作叫“排除这一轮”“固定”“预览模型输入”“带入新对话”。优先做可解释的手动选择、干净重建和跨会话包；暂不把主题识别、自动记忆和复杂会话树塞进首版。这样能先验证用户的控制权是否真正改变了请求，而不是只增加一个好看的面板。

本轮交付为核查和改造方案，尚未实现上述上下文功能，也未调用模型网关测试。本方案不依赖 Codex 官方文档。
