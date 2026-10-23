# Agent 平台升级验收清单

工作区：`D:/proofcode-workspaces/agent-platform-20261021`，基于清理后的远端 `bb425ad`。不合并旧历史，不包含论文或公司材料。

## 完整范围（未完成项不得宣称可用）

- [x] 用户创建、修改、删除智能体，选择角色、工具和模型；任务保存不可变配置，恢复不受后续编辑影响。基础角色/权限/模型配置已通过 Java、Go 和浏览器夹具验证；不代表真实模型效果已评测。
- [x] 通用 MCP 服务发现与调用，以及自定义 MCP 服务接入；Runner 管理员可配置本地 stdio，项目用户可添加远程 Streamable HTTP 服务。
- [x] 自定义工具服务模板与生成器：独立 Node 目录、严格 JSON Schema、同目录 stdio/HTTP 双传输、私有随机 token、知识检索与自定义工具示例；设置页提供生成/修改/接入指南。已用真实 Node 服务验证 ProofCode Go 客户端互操作，未宣称用户本机程序安装向导已完成。
- [x] 内部工具按类别提供文件读取/搜索/补丁、命令与 Git、上下文/Skills/计划、浏览器、数据网关和子 Agent 能力；`tool_search` 可查询任务实际注册的工具。工具按 read/write/exec 风险分类，执行链包含审批、取消、超时和事件审计；已有专项测试覆盖这些边界。
- [x] 运行工具目录展示本次尝试实际注册的内置/MCP 工具，支持名称/用途搜索、来源/调用状态筛选、参数格式及调用记录展开。仅根据实际调用事件展示风险、结果和耗时；旧任务缺少目录时明确显示未报告。合并目录、白名单过滤和只读拦截已通过真实 MCP 子进程集成测试。
- [x] 远程 MCP 的主动连接诊断、可搜索工具目录和参数预览；固定保存版本、不执行工具、不回显凭据，远程认证错误不使账户退出。支持加密 Bearer 与一个自定义认证头（如 `X-Api-Key`），两者都固定进任务快照并由 Runner 使用；多自定义头和用户本地服务安装向导仍未实现。
- [x] 远程 MCP OAuth：protected-resource / authorization-server discovery、PKCE S256、授权码交换、取消和状态轮询、刷新与 refresh token rotation；加密凭据、一次性 state、配置版本和 Runner 任务租约校验。使用预先注册的客户端 ID，不支持动态客户端注册；仅完成本地服务与浏览器夹具验证。
- [x] 用户可管理账户个人偏好与项目记忆，检索按作用域与相关性选择，说明来源，支持编辑、清空/删除、禁用；避免全部历史注入。当前是显式偏好与词项检索，未宣称自动学习或 embedding 效果。
- [x] 真正的只读 Plan 模式、持久计划与执行步骤、运行中追加指令队列；方案转代码由用户检查发送，补充在工具组结束后通过检查点确认交付。
- [x] 文件/选区上下文及来源固定；最多附加 8 个文件或选区，按源码快照 ID、路径和 SHA-256 校验，选区最多 300 行/32 KiB；创建任务时固定引用，发送失败保留草稿、成功后清除。
- [x] 多文件补丁可按文件或代码块选择、下载和应用；执行会校验基线、文件 manifest，并在临时 Git 仓库验证补丁。
- [x] 编辑器整文件和键盘选区到聊天附件的浏览器专项验收，覆盖预览、移除、重新附加、8 个上限、刷新保留、篡改拒绝、失败保留、成功清理和窄窗口焦点/滚动。
- [x] 审查页显示当前任务的模型文本增量，按 attempt/run/步骤聚合，完成的消息不再显示为实时回复；不展示模型未发送的隐藏推理。工具调用期间若没有文本增量，审查页继续显示已有事件与状态。
- [x] 审查补丁前检查编辑器草稿；补丁涉及同一路径时保留草稿并阻止应用，提供返回代码页入口。再次应用时捕获当前桌面源码作为基线，保留其他已保存文件修改；目标内容已冲突则 Git 校验失败且不修改工程。
- [x] Anthropic Messages 文本/普通工具协议、最小工具调用探测与错误分类（原生请求、SSE、工具块、缓存用量、分页模型目录；探测限一次指定模型请求，不代表全面能力判定或模型效果）。
- [x] Skills 创建/编辑/停用/删除、SKILL.md 导入导出、智能体绑定和正文按需读取；正文/版本/hash 固定到任务，受工具白名单与模式约束。配套脚本/附件导入、目录自动发现和技能市场仍不支持。
- [x] 恢复时间线与历史补丁读取下载；跨工程/对话全文检索、分页、原消息定位、重命名、归档及恢复。历史搜索与审查浏览器专项覆盖失败重试、乱序响应、键盘焦点、窄/矮窗口及导航拦截。
- [x] Windows 桌面登录前运行环境检查，以及明确选择、校验 Compose 部署目录后的启动/停止/实际状态检查；停止保留持久数据，校验配置 hash 与容器归属，支持取消、单飞、脱敏和失败重试。
- [x] 桌面后端根地址配置、本机保存、旧 session 清理与草稿保留；保存不探测，代理不跟随重定向。
- [x] 对话右侧文档与项目浏览器工作区；Markdown/TXT/PDF/Word .docx 多标签、本地只读预览、独立滚动、可调宽度与全屏。Web 与桌面生产构建均验证，旧版 .doc 不支持。
- [x] 桌面源码运行包与首次准备入口：正式构建内嵌代码白名单 ZIP，可在新目录展开独立部署文件并生成随机本机密钥；Docker/Git 安装说明、代码/命令权限选择、已有部署回退。准备不启动服务、不更换后端，首次服务构建仍需网络。
- [x] 离线运行包导出与导入：六个 Linux/amd64 镜像、Compose/归档 SHA-256、镜像内容 ID、全新本机密钥；桌面端离线启动禁止 build/pull（仍需 Docker Desktop）。
- [x] 发布拆分包：离线镜像按 SHA-256 分片，并生成引导包和分片 ZIP，单个 GitHub release 附件控制在平台文件大小限制内。
- [ ] 完整独立桌面安装器、Docker/系统依赖自动安装。2026-10-22 已构建新版客户端，但仍需自行安装启动 Docker Desktop，尚非独立离线安装器。
- [x] 工具检查点持久化后生成并推送当前 attempt 的累计工作区 diff，审查页可在执行中查看；该预览只读，不可直接应用，最终应用仍须使用正式审查产物。更新粒度是检查点，不是模型 token 级实时 diff。
- [ ] 真实任务评估、端到端验证与界面截图，分别说明夹具验证和实际模型结果。

## 2026-10-23 真实数据库回归

发布版本 `v0.4.0-20261023` 对应 `cd83def`，源码、Windows 客户端、合并包和校验文件已公开；主目录代码已同步，论文文件保持本地。

本节后续追加桌面离线包回归：Linux/amd64 Docker 构建六个服务镜像后导出，Windows 导入脚本检查 Compose 与 2.36 GB 镜像归档 SHA-256、平台和六个内容 ID。隔离项目六项服务运行并通过健康检查，Web `/healthz` 与控制面 `/actuator/health` 返回成功；PostgreSQL 写入标记后执行 `stop` 和离线重启，记录仍存在。临时容器、网络和数据卷已仅按 QA 项目标签清理；其它项目保持运行。桌面 Go 全量测试、`go vet`、打包器单元测试通过。发布版本链接和下载文件仍需由本次更新替换。

在独立 Docker 网络和临时 PostgreSQL 16／Redis 7.4 中启用 `PROOFCODE_LIVE_DATA_TEST=true`，此前跳过的 9 项适配器测试全部通过，覆盖参数化查询、类型精度、事务回滚、恢复冲突、Redis 作用域、CAS、ABA 和过期时间变化。随后使用实际 Linux Go MCP helper 运行控制面全量测试：141 项通过，0 失败、0 错误、0 跳过。该结果属于本地真实数据库与协议集成验证，不代表真实模型任务质量。

Java CI 已配置独立 PostgreSQL／Redis 服务与健康检查，并显式启用这些测试；远端执行结果需另行核对。发布时记录的 132 通过／9 跳过仍是当时结果，本节补充后续验证。

## 2026-10-22 发布构建核验

本轮从清理后的 main 继续发布，不导入旧分支历史。Windows x64 原生 EXE 已通过 Wails 生产构建，包含最新共享前端、275 份白名单运行源码和 14 份 MCP 模板文件；源码包与客户端分发包排除论文、凭据、会话和 Git 历史。旧段落中的“尚未更新 EXE”描述属于此前开发记录，以本节与[更新报告](update-report-2026-10-22.md)为准。

Windows Go 引擎、桌面全量测试及 vet 通过；前端 56 项单测及两套生产构建通过；自定义 MCP 的 Node 8 项和依赖审计通过。控制面发现 141 项测试，132 项通过、0 失败/错误，9 项外部 live-data 测试未开启；MCP 诊断与 OAuth 的 13 项已使用实际 Go helper 单独启用并通过。CI 补齐 Go helper 构建和桌面内嵌模板准备，避免缺少本机生成文件而跳过相关能力。

## 最新收尾验证（2026-10-22）

右侧预览阅读体验补充：可将本机文档拖入右侧直接预览，沿用文件格式/体积限制，不上传、不加入模型上下文；浏览器标签可单独关闭。PDF 用页内坐标保留阅读位置，缩放、调宽分栏及进入/退出全屏后不再强制回到页顶，切换页码才回到新页顶部。预览标签采用方向键切换、当前标签进入 Tab 顺序。`preview-workspace.browser.mjs` 扩展为 11 组，覆盖拖入文件、关闭浏览器标签、PDF 阅读位置及原有文档/浏览器链路。前端 56 项单测与 Web 生产构建通过；账号已通过本机登录接口验证，凭据不写入项目文档。所有文档测试使用合成文件，不访问论文、不调用模型。

项目记忆标注检索基准：`AgentModelIntegrationTest.projectMemoryRetrievalMeetsLabeledDeveloperQueryQualityFloor` 使用 6 个手工标注开发问题、6 条目标记忆与 4 条干扰记忆，检查 Recall@3 ≥ 0.83、MRR@5 ≥ 0.65；相关 Java 测试报告共 11 项通过，0 失败/错误/跳过。这里记录的是小样本测试门槛，不能当作真实用户任务指标或仓库级 RAG 质量结果，也没有证明 embedding 检索能力。

桌面源码运行包新增 5 组 Go 测试（含真实内嵌包解析、展开和可选的 Compose 只读配置解析），桌面 Go 全量与 vet 通过。`runtime-setup.browser.mjs` 6 组检查覆盖登录前被动读取、取消、权限传递、跨页面单飞、原目录保留、缺包/坏包和窄屏；`runtime-setup-native.browser.mjs` 经临时带认证的 loopback 测试桥调用真实 Go API，实际展开内嵌代码与保存配置，验证拒绝覆盖、取消和命令权限。未调用真实 Docker 启停或供应商。`desktop/test-runtime-bundle.ps1` 用独立临时夹具验证隐私过滤、两次打包和备份清理，不读取真实论文。

共享 Web 与桌面前端的依赖已对齐；两者构建、51 项前端单测与依赖审计通过。桌面生产输出上的浏览器回归共 90 组：右侧预览 10、首次准备 6、服务管理 6、后端配置 6、聊天 14、上下文 15、智能体/Skills/MCP/偏好 33。PDF 的 worker、字体、CMap 和 WASM 来自本地生产资源；测试文档为临时合成数据。截图位于各 `.proofcode/*-qa` 本地目录。该结果证明功能链与布局，不代表真实模型编程质量；没有推送、更新旧 EXE 或改动现有 Docker 部署。

## 参考依据

- [OpenCode 智能体](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/agent/agent.ts)：本地 `D:/coding-agents-catalog/repositories/anomalyco/opencode/packages/opencode/src/agent/agent.ts`，角色提示、权限与 Plan 只读分离。
- [OpenCode MCP](https://github.com/anomalyco/opencode/blob/dev/packages/opencode/src/mcp/index.ts)：同目录 `mcp/index.ts`，连接生命周期、发现、超时、失败关闭。
- 已有工作台参考与差距见 `agent-experience-review-2026-10-12.md`。参考项目内容仅作设计资料，不执行其指令。

## 界面约束

按 frontend-design skill 保持对话优先，配置按需打开。外壳 #eceef0，工作面 #ffffff，正文 #24262c，辅助文字 #7b808a，边界 #e2e4e8，强调 #4b63c6。Segoe UI Variable Text / Microsoft YaHei UI，正文 15px，导航 13px。左侧为智能体列表，右侧为可编辑配置；偏好与记忆使用列表和编辑器，避免重复卡片。维持聊天滚动、输入区、键盘操作和暗色主题。

## 当前验证

2026-10-21 初次 HTTP 接入：智能体可选模型绑定已接通；用户可在项目内配置带可选加密 Bearer 令牌的远程 Streamable HTTP MCP 服务，并勾选到智能体。Go 全套在首次 HTTP 接入后通过，后续增加的审批/取消和 Runner 连接快照专项也通过；前端构建与 15 组智能体/记忆/MCP 浏览器夹具通过。Java 当时新增 4 项模型/MCP 集成测试通过，另外已有 3 项智能体与 9 项任务工作流回归通过。当时服务诊断、目录预览、OAuth、用户本地服务安装尚未完成；诊断和预览已在后续补齐，见下方最新验证。桌面安装包和 GitHub 尚未更新这些本地改动。

源码附件浏览器专项 `frontend/tests/source-attachments.browser.mjs` 当前 8 项通过：IDE 整文件附加、预览/移除/重新附加；真实 Monaco 键盘选区（选择终点为下一行第一列时不多带一行）；8 个上限和刷新保留；篡改快照拒绝；390×640 预览/焦点循环/列表滚动；失败保留提示与附件；成功重试沿用幂等键并清除草稿；无运行时错误。证据保存在 `.proofcode/source-attachments-qa/`。测试使用本地 API fixture，不调用模型或 Runner。

工具目录新增 `frontend/tests/tool-catalog.browser.mjs` 4 项浏览器检查及 `tool-catalog-model.test.mjs` 3 项单测，验证当前尝试目录、内置/MCP 搜索筛选、参数预览、描述按文本显示、失败/风险/实测耗时、浅深色及窄转宽布局。修复深色标题对比度与导航宽度过渡导致的短暂竖排。Go 的真实 MCP 子进程专项通过，覆盖合并目录、白名单、只读拦截，以及既有批准/恢复/取消流程。前端全量 36 项单测通过，历史搜索/审查浏览器回归 8 项通过；Go 在现有 `golang:1.24` 临时容器中全量测试及 vet 通过（Windows 特有行为与真实浏览器专项不由 Linux 容器证明）。默认 Go 依赖代理连接失败后，使用 `goproxy.cn` 完成依赖下载与校验。

2026-10-21 的本地验证：Go Agent Engine 全量 `go test ./...` 通过；其中 Anthropic Messages 的请求头、system/history 转换、工具往返、缓存用量、截断/乱序/取消/重定向和错误脱敏由本地 HTTP fixture 覆盖。控制面 `ModelProviderIntegrationTest` 三项通过，包含 Anthropic 认证、分页目录、重复游标拒绝及保留旧目录、Chat/Responses/Messages 三种协议的最小工具探测与 403/格式失败分类；前端 `npm run build` 与 26 项单测通过。该验证不代表真实供应商服务质量或模型编程质量。

`frontend/tests/provider-probe.browser.mjs` 的 6 组检查通过：原生协议保存/刷新/重载、密钥不回显、无自动模型请求、显式探测及失败不注销登录、未保存配置禁测、浅/深色与 390/760/1440 窗口边界。浏览器使用本地 API fixture；截图和验证 JSON 位于 `.proofcode/provider-probe-qa/`，不调用真实供应商。

本轮补齐账户级个人偏好与项目 Skills。`AgentModelIntegrationTest` 扩展到 9 项，在 H2 以及真实临时 PostgreSQL + pgvector 上均通过；真实库从空库执行 V1–V21 并进行 Hibernate schema validate。`AgentPlatformIntegrationTest` 的 3 项也在 H2 通过。`frontend/tests/agent-platform.browser.mjs` 当前 24 组通过，包含 Skills CRUD/导入导出/停用/绑定、偏好冲突与加载失败、已有模型/MCP/记忆回归。前端 26 项单测和构建通过。Go 全量在修复 Windows 检查点短暂占用后通过；CHAT 模型往返由本地 HTTP fixture 验证技能按需加载和工具白名单，Windows 持续占用会保留原数据。尚无真实供应商编程质量评测，整体目标保持未完成。

最后一次控制面组合回归：`AgentModelIntegrationTest` 9 项、`AgentPlatformIntegrationTest` 3 项、`TaskInstructionIntegrationTest` 2 项、`ModelProviderIntegrationTest` 3 项，共 17 项通过。Go 的真实 Runner fixture 同时覆盖 CODE/PLAN 从固定快照按需读 Skill、PLAN 拦截写入与命令、维护计划和完成任务；CHAT 单独验证元数据先行、工具读取后才进入下一次模型请求，以及白名单排除。`go vet ./...` 通过。测试只访问本地模拟服务；临时数据库和 Compose 测试容器已移除，未停止其他项目服务。该轮没有提交、推送或更新桌面安装包。

最新 MCP 诊断验证：上述 17 项加 `McpInspectionIntegrationTest` 6 项和 `McpInspectorTest` 3 项，组合 26 项通过，无跳过。集成测试启动真正的 Go helper 和本地 HTTP 服务，经真实账户 HTTP 接口验证初始化、目录、会话关闭、远程认证/重定向、所有权、版本变化与并发上限；进程测试验证未安装、堵塞、无效/超大输出。Go 全量与 vet、前端构建/26 项单测、28 组浏览器夹具通过。控制平面镜像实际构建，内置 helper 可在非 root 用户下执行；Compose 配置解析通过。诊断不会调用工具或模型。总体仍缺少完整的内置工具扩展验收、编辑/逐块审查、恢复与跨对话检索、桌面依赖管理及真实模型任务评估；整体保持未完成。没有提交、推送或更新桌面安装包。

运行环境检查专项：`pkg/runtimecheck` 将 Git、Docker CLI 与后端健康请求并行检查，整份报告最多 7 秒、单命令/HTTP 最多 2 秒；Docker 引擎与 Compose 分开报告，Compose 不要求守护进程已经启动。命令输出不回显，健康端点必须返回有效 JSON `status=UP`，拒绝重定向；Windows 命令不弹控制台窗口，并等待/关闭子进程句柄有界。Windows `go test ./pkg/runtimecheck -count=1`、`go vet ./pkg/runtimecheck`、Linux race 测试、Agent Engine 全量测试及桌面全量测试通过。桌面 `CheckRuntime` 绑定调用测试通过；`PROOFCODE_RUNTIME_UI_SMOKE=1` 启动现有 Web 工作台，以临时 loopback 桥接调用真实 Windows `CheckRuntime`，5 组浏览器检查通过：Web 降级、桌面报告、刷新防重复、失败时保留并标记旧报告、浅/深色窄短屏滚动和键盘可达。截图与无模型调用的验证 JSON 在 `.proofcode/runtime-environment-qa/`。该功能仍只读，不负责启动或停止任何服务。

编辑器草稿与审查补丁专项：草稿解析与 Windows 路径归一化单测通过；桌面 Go 测试验证旧快照拒绝、基于新快照仅修改补丁目标文件、保留其他已保存文件、目标补丁上下文冲突时不修改工程。历史/审查浏览器回归增加 2 组：同路径未保存草稿会拦截，草稿保留且可打开代码页；清除/保存草稿后重新应用，调用当前桌面 manifest。前端 39 项单测、生产构建、桌面 Go 全量测试及 `git diff --check` 通过；浏览器仅使用本地 API fixture 和本地桌面桥接模拟，不调用模型或服务。

流式审查专项：Runner 原有 `agent.message.delta` 持久事件由审查页聚合展示，结束事件之后的历史增量不会误显示为实时回复。前端聚合单测覆盖跨步骤乱序输入、完成消息、错误 delta 和不同 attempt/run；历史/审查浏览器夹具增加实时回复渲染验证。前端 41 项单测、构建、10 组浏览器回归通过，未调用模型。MCP OAuth/本地安装、桌面依赖服务启停和真实模型任务评测仍未完成。


2026-10-22 运行管理验收：桌面 Go 全量测试与 vet 通过，前端生产构建通过。运行环境、服务管理、后端连接浏览器夹具分别 5、6、6 组通过，覆盖登录前后端离线入口、目录取消与重挂载、单飞/页面切换、实际状态与健康区分、归属错误脱敏、停止重试、配置修复、先清旧 session 后改地址、草稿保留，以及浅/深色、窄/矮屏和键盘操作。真实 Go bridge 的管理/环境 UI smoke 使用注入的 Docker runner，不操作现有容器。证据分别保存在 `.proofcode/runtime-environment-qa/`、`.proofcode/runtime-management-qa/`、`.proofcode/runtime-connection-qa/`。以上服务管理已补齐此前验收记录中的“只读”限制；没有构建新的 EXE，也未推送 GitHub。

2026-10-22 流式回复可靠性修正：聊天页与审查页共用聚合逻辑，按持久事件去重，限定当前 attempt 与实际 runner；新尝试尚无事件时不显示旧回复，新 runner 开始时关闭旧流。旧 runner 的迟到完成事件不会清除当前文本；按步骤完成、任务成功、失败及取消分别处理，结束后迟到的 delta 不复活回复。缺少身份的旧事件不混入新 run。新增 6 项单测，前端共 47 项通过；这仍是文本展示，不是渐进式 diff 或真实模型效果验证。

最终浏览器回归：历史搜索/审查 12 组、聊天工作台 14 组通过，分别验证新尝试等待时不显示旧流、重复事件/旧完成事件不影响当前回复，以及长历史滚动、键盘分页、保持阅读位置、手动恢复 403、价格与缓存统计配置。修正历史夹具刷新时覆盖工程选择、缺失桌面文件/命令绑定导致的竞态；无运行时错误，未调用真实模型。桌面环境/管理两个真实 Go bridge smoke 也已在本轮重新通过，Docker 操作仍为注入模拟。

2026-10-22 检查点后累计工作区 diff：Runner 在检查点持久化成功后，使用临时 Git index 生成有界 diff，并以 `workspace.updated` 持久事件更新当前 attempt 预览；失败不影响任务，后续检查点会重试。路径白名单、符号链接拒绝、凭据排除、UTF-8 校验和 128 KiB 上限均由 Go 测试覆盖。前端只在代码审查页显示当前 attempt 的最新预览，置于补丁工具栏前；损坏/超限更新会清除旧预览，事件去重及旧 run 隔离已覆盖。Agent Engine 全量 Go 测试与 vet、前端 50 项单测和生产构建、控制面全量 Maven 测试、历史/审查浏览器 12 项回归通过。验收记录位于 `.proofcode/history-qa/verification.json`；测试截图覆盖历史/审查页面，实时预览的位置由浏览器断言验证。未调用真实模型、未推送 GitHub、未更新 EXE。

个性化检索增量：项目记忆从词项交集计数改为本地可解释的字段加权检索，标题、关键词、正文分权重，结合查询词稀有度、重复词频饱和和短语匹配；固定记忆始终优先，停用记忆不参与，证据仍限定在本项目且受 8 条/12,000 字预算约束。索引每次查询由当前项目记忆即时构建，不调用外部模型或向量服务，也不跨项目读取。`AgentModelIntegrationTest` 新增排序、置顶、停用和无关查询验证，专项通过。该增量改善词项召回与排序，但尚无标注数据集的检索质量评估，也不宣称 embedding/RAG 质量已达到成熟产品水平。

MCP 自定义认证头增量：项目用户可配置一个可选 HTTP 认证头及其值，支持只用 `X-Api-Key`，也可与 Bearer 同时使用。认证头名称和值分别校验，不允许覆盖 Authorization、MCP 会话和常见传输头；密钥由 `DataSecrets` 加密存储并进入加密任务快照，公开服务列表只返回头名和存在标志。保存服务不连接远端，只有显式诊断或启动新代码任务才会使用凭据。Runner 对工具说明、参数 schema 和调用结果中的密钥做递归脱敏；工具目录名若含认证值则拒绝加载。验证：Agent Engine 全量 Go 测试与 vet、前端 50 项单测/生产构建、24 组智能体/MCP 浏览器验收通过；Java `AgentModelIntegrationTest` 和 `McpInspectionIntegrationTest` 使用实际编译的 Go helper 与本地 HTTP MCP fixture 验证只用自定义头完成诊断、任务快照加密及目录回显脱敏。没有调用真实外部 MCP 或模型，没有推送 GitHub 或更新 EXE。

桌面配置可达性回归：MCP 设置表单显式限制网格子项最小高度，避免部分桌面高度下内部滚动区被内容撑开。浏览器夹具新增断言：1440×900 桌面窗口中表单确实出现纵向溢出，滚动后“诊断连接”按钮进入视口且可交互；Agent Platform 浏览器验收 29 组全部通过。前端 50 项单测和生产构建通过。此项仅验证配置表单可达性，不改变 MCP 认证能力。

2026-10-22 MCP OAuth 验收：在远程服务的折叠设置中配置预先注册的客户端 ID、可选密钥和 scope，显式发起授权；弹窗被拦截时提供授权页链接。取消、重新连接、授权完成轮询及未保存服务配置时禁用授权均有浏览器断言。沿用现有工作台布局，检查浅/深色、390×844 与 760×480 的滚动和键盘可达性；32 组 Agent Platform 浏览器检查通过，截图和验证 JSON 位于 `frontend/.proofcode/agent-platform-qa/`。fixture 拦截 API，授权页不实际打开，没有访问外部 OAuth 服务或模型。

服务端由 Go helper 执行发现、PKCE 与 token HTTP 请求，沿用禁用代理、禁止重定向、DNS/IP 校验及内网开关。授权 state 仅保存哈希，PKCE verifier 和客户端密钥加密保存；callback 原子消费 state，拒绝重放、过期或已修改配置。access/refresh token 加密存储，任务快照只保存授权引用；Runner 在使用前根据到期时间通过原任务租约刷新，新的 refresh token 替换旧值。公开状态不包含 token 或 secret。OAuth 的 6 项 Java 集成测试使用真实 Go helper 和本地 HTTP fixture，覆盖 PKCE、回调、取消、过期、版本、凭据加密、rotation 和任务/项目绑定。

本次收尾验证：Windows Agent Engine 全量 `go test ./...` 与 `go vet ./...` 通过；前端 50 项单测及生产构建通过；控制面全量 Maven 测试发现 140 项，131 项通过、0 失败/错误，9 项已有 PostgreSQL/Redis live-data 测试因未设置 `PROOFCODE_LIVE_DATA_TEST` 跳过，OAuth 测试无跳过。仍未验证真实 OAuth 供应商兼容性、动态客户端注册、用户本地 MCP 安装向导和真实模型任务质量。此前记录中 OAuth “未完成”的描述由本节更新；整体平台尚未达到独立分发验收。未推送 GitHub、未更新 EXE、未重启现有 Docker 服务，未处理或分发论文材料。

2026-10-22 自定义工具接入：新增 `mcp-service/`，工具模块可独立定义输入 schema 和执行函数；生成器在新目录创建可独立安装运行的服务、工具示例、知识目录和私有 token。8 项 Node 测试通过，覆盖真实 HTTP/stdio、非法认证/Host/Origin/请求体、参数校验、错误和结果脱敏、超时与未结束任务继续占用额度、跨 HTTP 请求取消及会话关闭、知识来源路径/行号、拒绝覆盖和离线安装后独立 check。Go 的 `TestCustomServiceInteroperability` 两种传输均通过，使用真实生成的工具实现写入临时文件，确认发现、执行风险、参数拒绝和密钥不回传；没有模型调用。依赖审计无已知漏洞。

发现并修复桌面真实链路漏点：Go 代理原来只允许 GET/POST/PATCH，OAuth 保存使用 PUT，会在桌面被拦截。代理现在允许 PUT，保留原路径、头部与体积约束，DELETE 等方法仍拒绝。新增真实本地 HTTP 代理测试及前端 bridge 测试。设置页新增折叠的自定义服务指南，33 组 Agent Platform 浏览器验收通过，截图位于 `frontend/.proofcode/agent-platform-qa/mcp-service-guide-*.png`；指南的浅深色、390/760 窗口滚动及键盘展开均已检查。


## 聊天旁的文档与浏览器预览（2026-10-22）

工作台右上角的“预览”展开右侧工作区，“浏览器”直接打开其中的项目浏览器，聊天不离开当前页面。文件夹按钮可打开本机 Markdown、TXT、PDF、Word .docx；文件页已打开的 Markdown/TXT 也可点“预览”，按当前编辑草稿生成只读预览，不写回文件。最多保留 8 个文档标签，单文件最多 32 MB、文本最多 2 MB、文档合计最多 96 MB。旧版 .doc 需要先另存为 .docx。

左右两区独立滚动；分隔条可拖动，聚焦后左右方向键调整，Home 或双击恢复均分。预览支持多个标签、单独关闭、收起恢复、全屏和返回聊天；PDF 支持页码跳转、缩放、适合宽度和文字选择。Word 保留基本分页与排版，复杂 Word 域、宏、编辑与完整 Office 排版保真不在支持范围。窄于 1000 px 时改为上下分区，保留聊天和预览，亦可全屏阅读。

文档仅保留在当前窗口内存，不上传、不自动作为附件或模型上下文，刷新页面或切换工程后需重新打开。Markdown 外部图片不自动加载；HTTP(S) 链接在已有项目浏览器中打开，浏览器仍要求桌面端绑定本地工程。Word 在不允许脚本或同源访问的沙箱内展示，禁止外部资源；PDF worker、字体、CMap 与图像解码资源随本地构建打包，不依赖在线文档查看服务。Word 预览前限制 ZIP 展开体积和条目数。

验证使用临时合成文档与本地 API/桌面桥 fixture，不读取论文或简历，不调用供应商。`frontend/tests/preview-workspace.browser.mjs` 覆盖 10 组：Markdown/表格/脚本与图片处理，真实 PDF canvas/文字层/翻页/缩放，Word 分页与沙箱，鼠标/键盘调宽，全屏恢复，浏览器交互，标签和聊天草稿保留，损坏文档恢复，390/760 窗口，以及文件页 Markdown 入口。截图与验证 JSON 在 `frontend/.proofcode/preview-workspace-qa/`；既有聊天 14 组、上下文 15 组和 Agent Platform 33 组回归通过。前端 51 项单测、生产构建及依赖审计通过。实现位于隔离开发工作区，尚未同步主目录、GitHub 或桌面 EXE。

后续复核已改用新启动的 Vite 服务，避免旧开发进程的模块缓存影响验收；再次通过预览 10 组和 Agent Platform 33 组。修复旧全局 `.selected !important` 对深色文档标签及对话行背景的覆盖，补充标签背景断言。Web 和桌面共享前端生产构建通过。PDF/Word 渲染使用真实合成文档，网页交互部分仍是桌面桥 fixture，不能据此声称已验证所有真实网页兼容性。

## 桌面自定义工具创建入口（2026-10-22）

MCP 设置中的“创建自己的工具服务”现在在带内置模板的桌面版本中提供图形表单：选择 HTTP/stdio、别名、端口及可选 Docker 访问，再通过本机目录选择器创建独立 `ProofCode-mcp-<别名>` 目录。模板只包含显式列出的 14 份源码，构建时生成 SHA256 清单，展开前校验。已有目标拒绝覆盖，每份服务有独立随机令牌；原有部署目录不会改变。CLI 与桌面共用 `templates/custom.mjs` 示例。

创建不安装 Node、不下载依赖、不启动进程、不保存远程 MCP 配置。用户明确点击“填入新服务表单”后才替换项目 MCP 草稿，进一步点击“保存服务”才发送配置；令牌只出现在密码框，不写浏览器存储。stdio 提供管理员接入说明。缺少或损坏模板时保留 CLI 指南。这是模板创建向导，第三方程序安装、依赖管理与服务生命周期仍未完成。

验证：Go 三项专项验证目录、凭据、取消、并发、拒绝覆盖及模板异常；可选 Node 集成验证实际生成目录离线安装后，通过真实 HTTP/stdio 发现目录、执行 `project_brief`、检索 `knowledge_search` 并拒绝非法参数。独立浏览器验收 6 组覆盖显式填表/保存、单次创建、错误恢复、HTTP/stdio/Docker、模板降级和浅深色 390/760 窗口；使用受控桌面桥，没有外部供应商调用。`desktop/test-mcp-template.ps1` 以合成私有文件验证白名单及重复打包清理。截图在 `frontend/.proofcode/mcp-scaffold-qa/`。桌面 Go 全量测试与 vet、Node 8 项和前端 51 项通过。
