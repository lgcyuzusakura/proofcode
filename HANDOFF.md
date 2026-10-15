# ProofCode 接手说明

更新日期：2026年9月29日

## 1 用户的真实目标

用户是长春大学计算机科学与技术专业本科生。他不希望只做一个应付毕业设计的演示项目，而是要开发一套自己日常敢使用、可以作为简历核心项目、与 Codex、OpenCode、Claude Code、Pi 等产品有差异的 AI 编程 Agent，并同时完成中国本科毕业论文。

用户已经明确授权直接开发到 v0.4，并允许使用 Docker。用户不想使用 Rust，除非某个很小的底层模块有不可替代且明显的收益。当前方案不需要 Rust。

当前线程已创建持久目标，目标内容为：

> 开发一个达到 v0.4 的可日常使用、Web 与桌面双端 AI 编程 Agent，包含 Go Agent Engine、React UI、Java 控制面、PostgreSQL/pgvector、Redis、ActiveMQ Artemis、混合 RAG、受控多 Agent、Git Worktree、浏览器验证、MCP 扩展、测试与部署，并按长春大学计算机科学与技术本科要求完成并验证毕业论文 DOCX。

接手后先调用 `get_goal` 查看状态，目标当前应为 `active`。不要提前标记完成。

## 2 产品定位和关键差异

项目暂名 ProofCode。定位是本地优先的可验证编程 Agent。差异点不是模型数量，而是每个任务形成一个可审计的 Task Capsule：

- 任务计划和被选择的上下文证据
- 所有模型、工具和审批事件
- 独立 Git Worktree 中的变更
- Patch 和 Checkpoint
- 测试、构建和浏览器验证结果
- Token、费用和耗时
- 恢复、回滚和重放信息

核心执行流程：

```text
Scan -> Retrieve -> Plan -> Worktree -> Patch -> Verify -> Review -> Accept
```

受控多 Agent 只有三个角色：

- Main Agent：唯一拥有写权限
- Scout Agent：只读检索和影响面分析，最多两个
- Verifier Agent：只读审查、测试和浏览器验证

简单任务必须保持单 Agent。跨模块任务才启动 Scout。所有完成的修改都进入 Verifier。第一阶段禁止多个 Agent 同时写同一工作区。

## 3 已确定的技术架构

### 桌面本地模式

```text
React UI + Wails + Go Agent Engine
```

桌面模式直接使用本地 Git 仓库和系统 Chrome 或 Edge，不强制用户安装 PostgreSQL、Redis 或 ActiveMQ。桌面离线存储可以使用 SQLite；这只是嵌入式适配器，不改变 PostgreSQL 是服务端主数据库的设计。

### Web 和团队模式

```text
React Web
  -> Java Spring Boot Control Plane
  -> PostgreSQL + pgvector
  -> Redis
  -> ActiveMQ Artemis
  -> Go Runner
  -> Docker workspace
```

职责边界：

- Go Agent Engine：模型循环、工具、检索、Worktree、Checkpoint、浏览器、MCP、多 Agent
- Java Control Plane：项目、任务、权限、可靠入队、Runner 管理、WebSocket 事件
- PostgreSQL：唯一持久事实库和云端向量索引
- Redis：取消标记、临时锁、Runner 心跳、限流和短期缓存
- ActiveMQ Artemis：可靠的低频后台命令、重试和死信
- 高频 Token 和工具事件：Runner 到 Java 使用流式连接，再由 WebSocket 发给浏览器，不走 MQ

消息可靠性使用 Transactional Outbox。任务记录和 Outbox 在同一个 PostgreSQL 事务中写入。Runner 必须以 taskId 幂等消费。

## 4 当前工作区和环境

主项目目录：

`C:\Users\Administrator\Documents\Codex\2026-09-29\w-s\outputs\proofcode`

临时资料目录：

`C:\Users\Administrator\Documents\Codex\2026-09-29\w-s\work`

参考仓库目录：

`D:\coding-agents-catalog\repositories`

重要参考仓库均已存在：

- `openai\codex`
- `anomalyco\opencode`
- `badlogic\pi-mono`
- `charmbracelet\crush`
- `Aider-AI\aider`
- `SWE-agent\mini-swe-agent`
- `cline\cline`
- `aaif-goose\goose`
- `can1357\oh-my-pi`

本机环境：

- Docker Desktop 正常，Server 29.6.2
- Node 24.18.1
- Java 17.0.19
- Git 2.55.0
- ripgrep 15.0.0
- 没有全局 Go
- 没有全局 Maven

Go 和 Maven 构建均使用 Docker。国内网络下 Go 模块建议设置：

`GOPROXY=https://goproxy.cn,direct`

## 5 已创建的代码和文档

项目根目录已有：

- `README.md`
- `.env.example`
- `.gitignore`
- `Makefile`
- `compose.yml`
- `compose.test.yml`
- `protocol/events.schema.json`
- `docs/architecture.md`
- `docs/security.md`
- `docs/acceptance.md`

### Go Agent Engine

已创建并通过一次完整单元测试的模块：

- `internal/event`：版本化事件和线程安全序列号
- `internal/model`：Provider 接口与 OpenAI-compatible SSE 工具调用流解析
- `internal/workspace`：工作区路径和符号链接越界防护
- `internal/tool`：注册表和六个主要工具
  - `read_file`
  - `list_files`
  - `search_code`
  - `apply_patch`
  - `run_command`
  - `git_diff`
- `internal/agent`：有步数上限、取消、审批、工具审计的 Agent Loop
- `internal/context`：代码分块、RRF 混合检索融合和上下文打包
- `internal/worktree`：创建、Checkpoint 和安全清理 Git Worktree
- `internal/agent/coordinator.go`：Main、Scout、Verifier 协调及一写者规则
- `internal/browser`：基于 chromedp 的 open、snapshot、click、type、console、screenshot
- `internal/mcp`：stdio JSON-RPC 客户端、initialize、tools/list、tools/call

依赖已锁定到兼容 Go 1.24 的 chromedp 0.13.7。最新 chromedp 在当前时间点要求 Go 1.26，不要无意升级。

最近一次完整 Go 测试在添加浏览器和 MCP 后通过。之后修复了两个并发问题：

- Browser 执行锁与 Console 锁分离，避免回调死锁
- MCP 增加写锁和安全关闭 `done` channel

这两个修复之后尚未再次运行 gofmt 和测试。接手后的第一条检查应为：

```powershell
docker run --rm -e GOPROXY=https://goproxy.cn,direct `
  -v "${PWD}\outputs\proofcode\agent-engine:/src" `
  -w /src golang:1.24-alpine `
  sh -c "gofmt -w . && go mod tidy && apk add --no-cache git >/dev/null && go test ./..."
```

### Java Control Plane

已创建：

- Spring Boot 3.4.5 Maven 工程，Java 17
- Spring Web、WebSocket、Security、JPA、Redis、Artemis、Actuator、Flyway
- PostgreSQL 初始迁移
- `projects`、`tasks`、`task_events`、`outbox_messages`
- `code_chunks`，含 pgvector、tsvector、pg_trgm、HNSW 索引
- `symbol_edges`
- 项目创建和列表 API
- 任务创建、列表、查询、取消 API
- Task 与 Outbox 同事务写入
- Outbox 定时发布到 Artemis
- Runner 内部事件写入和幂等 sequence 处理
- 原生 WebSocket 任务广播
- 静态开发 Token 和独立 Runner Token

Maven 首次测试正在下载冷依赖时被用户要求切换模型，因此已主动中断，当前没有 Java 编译成功结论。下一位必须重新运行：

```powershell
docker run --rm `
  -v "${PWD}\outputs\proofcode\control-plane:/src" `
  -v proofcode-maven-cache:/root/.m2 `
  -w /src maven:3.9-eclipse-temurin-17 `
  mvn test
```

预计需要处理的 Java 测试问题：

- 测试配置排除了 Redis 自动配置，但 `TaskController` 仍依赖 `StringRedisTemplate`，ContextLoadTest 可能需要 `@MockBean`
- `spring-boot-starter-artemis` 的 embedded 测试模式可能缺少 broker 依赖，测试中可以 Mock `JmsTemplate`
- H2 对 `jsonb` columnDefinition 和 PostgreSQL 专属类型兼容有限；可以把 ContextLoadTest 改成 Testcontainers PostgreSQL，或将仓储测试与应用上下文测试拆开
- 先以真实 PostgreSQL Compose 集成测试为准，不要为了 H2 修改正式 PostgreSQL 数据模型

### 尚未实现

- `cmd/runner`，因此 Agent Engine Dockerfile 当前还不能构建最终 Runner
- Artemis AMQP 消费和 Runner 到控制面的事件流
- PostgreSQL/pgvector 的真实 Go 检索 Store 和 Embedding API
- 远程审批等待和继续执行
- Task Capsule、Artifact 和 Checkpoint 的服务端持久化 API
- React Web 前端
- Wails 桌面端
- Docker 强隔离参数和 Runner 镜像中的 Chromium
- MCP 工具动态注册到 Tool Registry
- 端到端 Compose 测试
- CI、安装包和性能基准
- 论文 DOCX

`compose.yml` 已经引用 control-plane、runner 和 frontend，但 runner 与 frontend 尚未完整，所以当前 Compose 不能作为完成状态启动。

## 6 长春大学官方论文资料

已经从长春大学教学信息网官方页面取得 2024 届资料。

官方通知：

`https://jwc.ccu.edu.cn/info/1033/3620.htm`

毕业论文栏目：

`https://jwc.ccu.edu.cn/sjjx/bylw.htm`

官方附件压缩包：

`work\ccu-2024-attachments.zip`

通过官网验证码正常下载，未绕过权限。已用 GBK 文件名编码提取以下三个重要文件：

- `work\ccu-official\0 长春大学毕业设计（论文）工作指导书.doc`
- `work\ccu-official\2.2 长春大学毕业设计学生手册（食品科学与工程学院、 计算机科学技术学院、  理学院 、园林学院）.doc`
- `work\ccu-official\吉林省本科毕业论文(设计)抽检评议要素.doc`

学校通知已核实的硬性要求：

- 完整结构必须包括封面、标题、中英文摘要、目录、正文、致谢、参考文献
- 绪论必须包括研究背景与意义、国内外研究现状、研究内容与研究方法
- 设计类说明书不少于 1.2 万字
- 理工类论文全文不少于 1.5 万字
- 学校使用维普系统进行全过程管理和查重
- 具体格式应按《长春大学本科生毕业设计（论文）工作管理规定（修订）》附件 3 和计算机科学技术学院适用学生手册执行

下一步需要使用 bundled LibreOffice 将 `.doc` 转成 `.docx`，完整读取计算机学院学生手册中的页面、字体、字号、行距、标题、图表、公式、参考文献和封面要求。不要凭常见高校格式猜测。

## 7 论文制作技能要求

本轮已启用并完整阅读 Documents skill。路径：

`C:\Users\Administrator\.codex\plugins\cache\openai-primary-runtime\documents\26.905.11957\skills\documents\SKILL.md`

同时已读：

- `tasks/create_edit.md`
- `tasks/verify_render.md`
- `references/template-elicitation.md`
- `writing_quality.md`

可用的官方工作区依赖：

- Node：`C:\Users\Administrator\.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin\node.exe`
- Python：`C:\Users\Administrator\.cache\codex-runtimes\codex-primary-runtime\dependencies\python\python.exe`
- pnpm：`C:\Users\Administrator\.cache\codex-runtimes\codex-primary-runtime\dependencies\bin\fallback\pnpm.cmd`

尚未开始 DOCX authoring，也没有运行 artifact marker。正式创建论文 DOCX 前必须恰好运行一次：

```powershell
<bundled-node> container_tools/mark_artifact_operation_started.mjs `
  --operation-kind create --expected-output-count 1 --output-format docx
```

论文必须执行：

1. 按官方手册建立格式
2. 使用 python-docx 或技能脚本生成
3. 使用技能自带 `render_docx.py` 渲染全部页面
4. 逐页查看 PNG，不能抽查
5. 修复布局后重新渲染
6. 最终仅交付 DOCX，不把 QA 图片当交付物

论文不能伪造尚未完成的实验数据。性能、成功率、检索指标和用户测试必须来自最终程序的真实基准测试。学生姓名、学号、指导教师、学院正式名称和提交日期当前未知，应使用明确的可编辑占位字段，并在交付时告诉用户替换。

## 8 推荐的继续顺序

1. 重新运行 Go gofmt 和全部测试。
2. 完成 Java Maven 编译，修复测试上下文，不修改正式 PostgreSQL 设计来迁就 H2。
3. 实现 `cmd/runner`、Artemis AMQP 消费、仓库克隆、Worktree、事件回传和幂等租约。
4. 实现 PostgreSQL Hybrid Store：精确搜索、FTS、trigram、pgvector 和 RRF。
5. 实现任务审批、Task Capsule、Artifact、Checkpoint 和恢复 API。
6. 实现共享 React UI，先打通 Web 创建项目、创建任务、WebSocket 事件、Diff、审批和设置页面。
7. 用 Wails 包装相同 UI，接入本地 Go Engine；不要把 PostgreSQL、Redis、ActiveMQ 强塞进桌面离线模式。
8. 完成浏览器验证和 MCP 动态工具注册。
9. 收紧 Docker：非 root、只读 rootfs、CPU/内存/PID、默认无网络、无 Docker socket、每任务独立目录。
10. 启动完整 Compose 做真实端到端测试，并添加失败、重复消息、取消、重启恢复测试。
11. 用项目自身完成至少三个真实任务进行 dogfooding，记录性能和缺陷。
12. 最后写不少于 1.5 万字论文，生成真实架构图、时序图、ER 图和实验表，再执行 DOCX 全页视觉检查。

## 9 开发原则和禁止事项

- 不要把空接口、TODO 或假返回结果写成“v0.4 已完成”。
- 不要把命令黑名单称为安全沙箱。桌面端应称为受控执行器，云端 Docker 才是隔离执行。
- 不要让多个 Agent 同时修改同一工作区。
- 不要让 MQ 承载逐 Token 高频流。
- 不要让 Redis 成为任务或会话的唯一存储。
- 不要在桌面模式强制启动全套中间件。
- 不要为了使用 Rust 而使用 Rust。Go 足以完成当前底层执行需求。
- 不要直接 Fork OpenHands 或 Codex；当前工程是独立实现，只选择性参考 Pi、Crush、Aider、Codex、OpenCode 和 mini-swe-agent。
- 保留用户工作区中的既有改动。所有编辑继续使用 `apply_patch`。

## 10 当前状态一句话

ProofCode 已完成架构定稿、Go Agent 核心和主要基础模块，Java 控制面代码已落盘但尚未完成首次编译；Runner、前端、桌面、端到端部署和论文仍需继续实现并验证。

## 11 本轮接手更新（2026年9月29日）

本轮已完成并验证：

- Go Agent Engine 使用 `gofmt` 后全量测试通过；新增 `cmd/runner`，使用 Artemis AMQP 1.0 消费任务，克隆 HTTP(S) 仓库，创建隔离 Worktree，运行 Main/Scout/Verifier，回传事件并创建 checkpoint。
- Runner 增加控制面任务租约领取、15 秒续租、取消状态轮询、失败/取消收尾事件；控制面新增 `tasks.runner_id`、`tasks.lease_until` 迁移和内部 claim/renew/status API。
- Java 控制面 Maven 测试通过。测试 profile 排除了嵌入式 Artemis/Redis 自动配置并使用 mock，生产 PostgreSQL/Redis/Artemis 配置未改变。
- React Web UI 已创建，支持项目创建、任务创建、任务列表、WebSocket 实时 Task Capsule 事件、取消和结果展示；前端生产构建通过。Nginx 已配置 API/WebSocket 代理。
- Compose 增加 Artemis AMQP 5672 端口、Runner 权限开关和前端服务；`docker compose config` 通过。Runner 和 Web 镜像构建通过；控制面镜像构建因首次 Maven 依赖下载长时间无响应而未完成镜像级验证，但 Maven 编译/测试已通过。

仍未完成：远程审批继续执行、真实 pgvector Store/Embedding、Task Capsule/Artifact 持久化、Docker Chromium 隔离、MCP 动态注册、端到端 Compose 烟测、CI/安装包和论文 DOCX。

## 12 桌面测试入口（2026年9月29日）

已新增 `desktop/start-desktop.ps1` 和 `desktop/stop-desktop.ps1`。启动脚本会构建或复用 `frontend/dist`，用本机 Python 启动静态服务，并用独立 Edge App 窗口打开共享 React UI；该预览已验证返回 200。随后通过阿里云 Go 镜像安装 Go 1.24.6、通过 `goproxy.cn` 安装 Wails 2.10.2，并生成真正的 Windows Wails 工程 `desktop/native`。`desktop/build-native.ps1` 构建成功，`desktop/start-native.ps1` 已启动 `desktop/native/build/bin/native.exe`，窗口标题为 `ProofCode` 且进程响应正常。
