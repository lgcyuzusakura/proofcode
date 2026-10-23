[简体中文](README.md) | [English](README.en.md) | [繁體中文](README.zh-TW.md) | [日本語](README.ja.md)

# ProofCode

ProofCode 是面向可验证代码修改的编程 Agent：读取仓库、生成补丁、执行经过批准的工具、运行测试，并交付可以审阅和恢复的修改产物。项目使用 Go Agent 引擎、Spring Boot 控制平面、共享 React Web/桌面界面和 Windows Wails 应用，服务于日常开发工作流。

核心流程是：**提交目标 → 隔离工作树 → 工具与批准 → 代码补丁 → 测试验证 → 审阅产物 → 应用到原仓库**。

## 2026-10-22 平台与桌面更新

自定义智能体、工具白名单、Skills、MCP HTTP/stdio 与 OAuth、账户偏好、项目记忆、只读 PLAN、执行中补充指令、历史检索和补丁分块审查已进入本次代码发布。对话右侧可并排查看 Markdown、PDF、Word 与项目浏览器，支持拖入文档、独立滚动、调宽和全屏；PDF 缩放和全屏切换保留阅读位置。

Windows 客户端内嵌最新共享界面、源码运行包和 MCP 模板，登录前可配置后端、检查环境、准备部署目录和显式管理 Compose 服务。仍需 WebView2；本机后端需 Docker Desktop，首次构建需网络，不能只填 API Key 单独运行。

[本次更新与验证](docs/update-report-2026-10-22.md) · [下载桌面版和源码](https://github.com/lgcyuzusakura/proofcode/releases/latest) · [平台运行说明](docs/agent-platform-runtime.md)

![聊天与文档并排预览](docs/images/preview-word-20261022.png)

截图使用合成文档和本地接口夹具；真实模型编程质量尚未完成评测。

## 2026-10-15 功能更新

- **上下文自动／手动选择**：自动沿用普通历史；手动勾选完整问答，支持排除、恢复、搜索、批量选择、撤销和人工摘要替换，切换模式保留手动选择，不额外显示编号轮次。
- **跨对话交接**：保存、挂载或卸载命名上下文包，在同一工作区带入新对话。来源变化后标记过期；任务创建时固定选择快照，重试与恢复复用原快照。交接的是文本材料，不迁移供应商缓存。
- **对话与执行动态**：独立滚动和输入区、按需加载历史、向上阅读位置保持；直接展示真实动作、状态和工具耗时，详情可展开，停止按钮调用真实取消接口。不会生成隐藏推理或将未知验证标为通过。
- **模型与用量**：按模型保存可选的上下文／输入／输出预算、压缩强度和价格区间；默认 128000／16000／2048 Token，另预留 256 Token 协议空间。支持自定输入区间、整档输入／输出／缓存读写单价；未填写价格仍可使用。
- **协议兼容**：模型服务支持 OpenAI Responses、Chat Completions 与 Anthropic Messages；Anthropic 的 SSE 工具块、缓存读写用量和分页模型目录经过本地模拟服务验证，目录刷新失败不会覆盖旧目录。
- **运行观测**：首字与总请求耗时、估算 Token/s、已报告的缓存用量与命中比例、Token 总量和估算费用。未报告数据保持未知，速度含传输开销，费用不替代供应商账单。
- **Windows 桌面**：Wails 使用共享 React 工作台；exe 需要 WebView2 和运行中的后端，尚不是只填写 API Key 的独立单文件服务。

[上下文更新与验证](docs/update-report-2026-10-14.md) · [执行动态与桌面更新](docs/update-report-2026-10-15.md) · [模型配置与计价](docs/update-report-2026-10-13.md)

![对话工作台与执行动态](docs/images/execution-activity-desktop.png)

截图为本地接口夹具渲染，不是实际模型性能结果。10 月 15 日记录为前端单元 26 项、执行动态 7 组、聊天工作台 14 组、上下文管理 15 组通过，Web/Wails 构建通过；未在该轮启动原生窗口，详见更新报告。

## 对话优先的开发界面

默认进入干净的对话页；审查是独立页面，每个代码回合用小型“查看修改”按钮准确跳转。Windows 桌面端增加真实 Monaco 编辑、版本核对保存与工程草稿恢复、本机命令控制台、Git 暂存/提交/分支/远程管理、结构化合并审查、标准 LSP/DAP 语言服务与断点调试，以及可点击输入和完整 Chromium DevTools 的独立 Edge/Chrome 预览。普通代码任务可选启用浏览器验证工具，A–F 对照配置保持固定。

[操作、实现边界与参考项目](docs/developer-workspace.md) · [结构化合并与 IDE 协议说明](docs/structured-merge-and-debugging.md) · [可视化数据与审查界面设计](docs/visual-data-and-review-ui.md) · [本轮验收记录](docs/verification-structured-merge-2026-10-09.md)


## 版本与开发状态

`main` 现提供 2026-10-22 智能体平台与桌面工作区更新；`codex/compact-context-agent-tools` 保留 2026-10-15 快照。其他分支保留此前的代码快照；各分支是独立快照，不共享旧提交历史。

| 分支 | 内容范围 |
| --- | --- |
| `main` | 智能体平台、Skills/MCP、上下文、文档预览与桌面运行管理 |
| `codex/compact-context-agent-tools` | 2026-10-15 工作台与上下文快照 |
| `codex/structured-merge-debug-tools` | IDE、合并审查与调试快照 |
| `codex/project-workspace-bootstrap` | 工程、数据与上下文集成快照 |
| `codex/visual-data-review-workspace` | 数据与审查界面快照 |
| `codex/backend-experiments-data-20261008` / `codex/backend-baseline-20261008` | 后端工作流基线 |
| 其他 `codex/*` 分支 | 早期实现或方案快照 |

## `main` 已实现的能力

- OpenAI-compatible 接口与工具调用；主循环支持步骤/用量预算、取消、审计事件和确定性模拟接口。
- 工作区内的文件读取、分页文件列表、`rg` 搜索、结构化补丁、受限命令与 Git diff。
- 每个任务 attempt 使用独立 Git worktree，保存检查点、完整消息及恢复产物。
- 条件式 Main、Scout、Verifier 协作；只有 Main 能修改文件，Scout/Verifier 使用只读工具。
- 可选 Kev/Jev 固定候选工具路由；它负责选择工具，工具参数仍由生成模型提供。
- Spring Boot 任务 API、数据库租约、取消/恢复、WebSocket 事件及 PostgreSQL 事务 Outbox。
- Go Runner 通过 Artemis AMQP 1.0 领取任务，执行仓库准备、工具和检查点回调。
- React Web 项目/任务入口、实时事件、批准与取消、任务产物时间线；Windows Wails 桌面界面原型与脱敏配置展示。
- `proofcode-apply` 在审阅后校验基线 revision 和干净工作区，再应用补丁。
- 浏览器验证可按需接入普通代码任务；MCP stdio 与远程 HTTP 工具已接入 Runner，详见本次平台更新。

## 当前实现分支

| 能力 | 实际实现 |
| --- | --- |
| 可编辑对话上下文 | 自动／手动选择、排除/恢复、搜索批量选择、手写摘要替换及输入历史预览；任务快照固定重试/恢复选择；命名上下文包可同工作区跨会话带入、卸载，来源修改后提示过期。[2026-10-14 更新报告](docs/update-report-2026-10-14.md)。 |
| 账户与模型服务 | 账户密码登录、注册、登出和会话过期；工程与模型服务按账户隔离；网页配置 Base URL、加密 API Key、Responses/Chat 协议，通过服务的 `/models` 读取模型目录并选择模型、编辑显示名称。 |
| 项目与对话 | 项目 → 工作区 → 对话 → 任务归属链；按项目/目录切换对话，持久保存用户与助手消息；普通聊天与代码任务分开执行。 |
| 无目录聊天 | 原生 Windows 桌面通过 Desktop KnownFolder 在真实桌面的 `ProofCode-Projects/<名称>_<UUID>` 创建工程；同一 bootstrap ID 可重试，路径仅保存在本机注册表。Web 可创建 SCRATCH 项目，但不能创建客户端桌面目录。 |
| 真实源码执行 | 原生端导入本地目录，捕获允许的当前文件字节，包括 dirty 和允许的未追踪文件；控制平面保存不可变 manifest，Runner 核对入队哈希后在隔离工作树执行。Web SCRATCH 的后续代码任务使用已保存的源码结果。 |
| 共享界面与传输 | Web 与 Wails 使用共享 React 工作台；原生 `/api/` 调用通过 Go bridge 访问控制平面，按持久事件序号轮询。Web 使用 WebSocket，并在断线时补取持久事件。 |
| 修改审阅与应用 | 展示真实 checkpoint/recovery diff；本地应用先核对原快照 manifest，在临时 Git 仓库验证补丁，检查目标路径及逐文件原字节，并保存本机应用日志与恢复记录。 |
| 多版本代码 RAG | 当前 generation 与显式历史 snapshot 分开查询；Go 使用 `go/ast`，其他语言明确回退到 line-regex；词项/符号/引用/测试倒排索引经带权 RRF 和覆盖预算选择，证据绑定文件版本、哈希和行号。 |
| 可回取压缩 | 原 transcript、工具 JSON 与约束台账先持久化；成功日志去重和完整协议组预算裁剪后保留 `context_read` 导航。全部用户要求、错误、批准/拒绝与 data 操作受保护，超预算明确失败。 |
| 数据库与缓存 | 独立 DataSession 管理会话；连接、结构、计划、审计和恢复按项目隔离。PostgreSQL 字段拖入单表查询画布、条件/排序/分页、结果编辑与受限迁移；Redis string 的 SCAN/GET/SET/DELETE/EXPIRE 与 TTL 管理。 |

数据读取、写入和恢复都经过“计划 → 预览 → 用户批准 → 执行”，普通任务的自动写入开关不能绕过。元数据检查、连接验证和只读 EXPLAIN 可在批准前进行；“批准前零执行”指查询计划、写入及恢复执行。PostgreSQL 事务内失败自动回滚；已提交行修改和 Redis 键通过加密快照生成新的条件恢复计划，必须再次批准。未知提交结果不自动重放。

### 实现边界

- PostgreSQL 尚不支持原始 SQL、JOIN、聚合、任意函数和存储过程；迁移仅开放受限建表、可空列和普通索引，尚无已提交迁移的通用自动 inverse。
- Redis 当前是 standalone 的受控命名空间 string 操作，不支持 Cluster、hash/list/set/zset 或任意命令；条件恢复不能替代跨数据库事务。
- 历史 RAG 只保存被系统捕获的版本，不会自动导入全部 Git 历史；没有 embedding、学习式重排或多语言 AST。热查询仍核验并扫描源码，尚未证明速度或准确率优势。
- 压缩保留原文不代表模型理解无损；Token 基于字节估算，真实 usage 单独记录。账户具备逐用户工程隔离，尚无团队共享、角色管理、找回密码或 MFA；兼容开发 Token 仅能访问未归属工程，Runner 使用独立凭据。
- 补丁应用日志支持恢复检查，但不能保证多文件写入的操作系统级原子性，也不能阻止外部程序同时改文件。

专项说明：[版本检索与上下文](docs/backend-context.md) · [数据工作台与恢复](docs/backend-data.md) · [实验与会话](docs/backend-experiments.md)。

聊天中的执行过程会直接显示动作、摘要、状态与工具耗时，可展开输入和输出；历史执行记录按需加载，底部可查看当前动作并停止任务。界面与验证见 [2026-10-15 更新报告](docs/update-report-2026-10-15.md)。

## 快速启动

需要 Docker Desktop 与 Docker Compose。真实编程任务还需要可用的 OpenAI-compatible 接口配置。

```powershell
git clone https://github.com/lgcyuzusakura/proofcode.git
cd proofcode
Copy-Item .env.example .env
```

在 `.env` 中设置自己的 `RUNNER_TOKEN` 和 `DATA_SNAPSHOT_KEY`（32 个随机字节的 Base64，用于模型密钥和数据恢复快照加密）。模型服务可在登录后的网页设置中配置；无账户的测试 Runner 才需要 `MODEL_BASE_URL`、`MODEL_NAME`、`MODEL_API_KEY`。然后运行：

```powershell
docker compose up --build
```

当前 Runner 使用任务的 `model` 字段。桌面从本机 Codex 配置读取 provider、profile、协议和模型目录，页面仅获得脱敏状态与模型 ID。同一用户运行的本机 Runner 在没有显式 `MODEL_BASE_URL` / `MODEL_API_KEY` 时可读取本机配置；Docker Runner 继续使用环境变量，不自动挂载凭据。仅修改 `.env` 的 `MODEL_NAME` 不会覆盖任务模型。

普通任务按需使用子 Agent，最多 4 个、并发 1、每个最多 6 次循环，子任务输入约 12k / 输出 1k。主任务与子任务可互发消息，只有主任务能写工程；原始观察先持久化，再生成有限模型视图。默认主任务上下文 128000、输入 16000、输出 2048 Token；界面参数可留空或按模型设置更大的上限（当前配置校验上限 2000000），实际 Token 和缓存输入仅根据 provider 报告展示。详见[上下文、协作与共享浏览器](docs/compact-context-agent-tools.md)。

打开 [http://localhost:3000](http://localhost:3000)。Compose 会等待 PostgreSQL、Redis、Artemis 和控制平面健康后再启动依赖服务。

首次打开网页时注册账户，再用账户名和密码登录。首个账户会认领升级前没有归属的工程；升级时已有唯一账户的，下一次成功登录也会补全认领。多个账户已存在时不自动认领遗留工程。点击「设置」添加模型服务，填写名称、API 地址和密钥，选择 Responses 或 Chat Completions，保存后「读取可用模型」；对话页从目录选择模型，不能手填不存在的模型 ID。已有密钥不会回显，更改服务地址时须重新输入密钥。桌面版还可直接导入本机 Codex 配置。

默认模型服务地址需解析到公共网络；管理员确需内网服务时可设置 `PROOFCODE_PROVIDERS_ALLOW_PRIVATE_HOSTS=true`。API 地址不接受嵌入凭据、查询参数或片段。服务须提供 `/models` 的 `data` 目录；Anthropic Messages 服务使用原生密钥认证并支持有界分页。设置页可手动验证选定模型的最小工具调用，会发送少量测试文本并可能产生模型费用，不发送项目内容、不执行返回工具；结果只证明该次请求的格式兼容。

端口冲突时，在 `.env` 中覆盖 `PROOFCODE_*_PORT`，例如 `PROOFCODE_WEB_PORT=3010`、`PROOFCODE_CONTROL_PORT=8090`。

### Windows 桌面

静态预览需要 Node.js/npm、Python 3 和 Edge/Chrome。原生构建需要 Go、Wails 2.10.2、Git 和 WebView2；默认工具目录为 `.tools/go` 与 `.tools/bin`，也可设置 `PROOFCODE_GO_ROOT` 和 `PROOFCODE_WAILS_BIN`。工具未随仓库分发。也可用系统 Wails 在 `desktop/native/` 内运行 `wails dev` 或 `wails build`。

```powershell
# 静态桌面界面预览
.\desktop\start-desktop.ps1

# 原生 Wails 应用，依赖 Go/Wails 环境
.\desktop\start-native.ps1

# 重建原生应用
.\desktop\build-native.ps1

# 停止静态预览服务器
.\desktop\stop-desktop.ps1
```

原生桌面已接通控制平面 API，并通过持久事件轮询读取真实任务进度。登录前可进入“运行环境与本机服务”，从内置源码运行包准备新目录，或选择已有 Compose 部署；服务启动与停止需显式操作。内置准备会生成 `.env`，宿主仍需安装启动 Docker Desktop，首次服务构建需要网络；新版 EXE 尚未发布。服务就绪后注册／登录账户并配置模型服务；`DEV_AUTH_TOKEN` 仅用于兼容的开发接入。原生代理默认连接 `http://127.0.0.1:8080`，地址优先使用页面保存值，其次 `PROOFCODE_CONTROL_PLANE_URL`，最后默认值。更换地址会退出旧账户，保留工程编辑草稿；保存不自动联网探测。静态预览只展示 UI，没有原生目录能力或控制平面代理。详细说明见 [桌面入口](desktop/README.md) 和 [原生应用](desktop/native/README.md)。

### 工具批准与运行环境

默认写文件和执行命令需要批准。`RUNNER_ALLOW_WRITE=true`、`RUNNER_ALLOW_EXEC=true` 分别允许自动文件写入与命令执行；它们不改变路径、命令和取消检查。

Runner 镜像包含 Go 1.24、Node.js/npm/npx、Python 3/pytest、Git 和 ripgrep。`RUNNER_ALLOWED_PROGRAMS` 配置可执行程序白名单；需要 Java、Rust、.NET 等工具链时扩展镜像和白名单。

Git worktree 提供修改隔离；当前长期运行的容器还没有完整的每任务网络、CPU/内存隔离。身份认证使用独立全局用户/Runner token，尚无团队成员 RBAC 或项目成员权限。现有边界见 [安全模型](docs/security.md)。

## 可选 Jev 工具路由

配置现有 TypeSafe-compatible 服务的 `JEV_BASE_URL`。`JEV_MODE=observe` 记录决策并保留全部工具；`JEV_MODE=route` 在置信度达到 `JEV_MIN_CONFIDENCE` 时限制本轮工具集合。普通任务遇到低置信度、非法响应或服务不可用时回退完整工具列表，并记录事件。默认未开启路由。

Jev 不生成工具参数、不批准操作，也不能绕过确定性策略。事件 `decision.tool_routed` 保留实际选择依据；置信度不等于实测正确率。后端实验分支的 C/F 组要求有效 Jev 路由，配置或服务失败时明确失败，不会静默变成另一组。

## 审阅并应用修改

Runner 的修改位于隔离工作树。审阅 `checkpoint` 或 `recovery` 产物后，针对基线 revision 一致且干净的原仓库执行：

```powershell
$env:PROOFCODE_AUTH_TOKEN = "<your-token>"
go -C agent-engine run ./cmd/proofcode-apply -url http://localhost:8080 -task "<task-id>" -repo "D:\path\to\checkout" -kind checkpoint
```

命令先执行 `git apply --check`，核对 revision 与工作区状态，通过后应用为未暂存修改。当前开发机若未配置系统 Go，可使用被忽略的 `.tools/go/bin/go.exe`；容器镜像也提供 `proofcode-apply`，可挂载目标仓库到 `/repo` 后调用。

上述 CLI 适用于 Git 仓库基线。当前原生本地工程可在真实 diff 审阅窗口中批准应用，通过上传时的文件 manifest 与当前字节核验来保护已有 dirty 修改；它不要求先把这些修改提交到远程仓库。

## 六组比较与项目数据能力

当前分支使用固定配置版本 `proofcode.experiment.v2-context`，提供可实际运行的六组对照：

| 组 | 配置 |
| --- | --- |
| A | 纯 LLM，Agent 无工具；独立 evaluator 应用其最终 diff 并测试 |
| B | LLM + 工具 + 额外确定性安全策略 |
| C | LLM + 工具 + 必需 Jev 路由 |
| D | LLM + 工具 + 版本化混合代码 RAG |
| E | D + 持久可回取上下文压缩 |
| F | E + 确定性安全策略 + 必需 Jev + 未解决失败反馈 + 项目数据工具 |

全部组保留工作区、路径、命令和数据批准底线。v1 与 v2 的算法和配置不同，不应混成同一批实验。可在独立目录取得当前版本：

```powershell
git clone https://github.com/lgcyuzusakura/proofcode.git proofcode-backend
cd proofcode-backend
docker compose -f compose.experiments.yml up --build -d runner
docker compose -f compose.experiments.yml run --build --rm verify
docker compose -f compose.experiments.yml down --volumes --remove-orphans
```

夹具使用独立服务与可销毁数据库，不发布宿主端口。A–F 使用固定源码版本、独立任务与工作树、真实补丁和统一测试，再导出 JSON/CSV。报告写入 `.tools/experiment-e2e/`；这是工程链路验证，正式论文对比需要真实响应与标注任务集。

数据连接保存 `secretRef`，凭据由控制平面的 `DATA_SECRET_<引用>` 环境变量解析。Compose 提供 `DATA_SECRET_PG_DEV` 与 `DATA_SECRET_REDIS_DEV`；PostgreSQL 行修改和 Redis 恢复快照需要 `DATA_SNAPSHOT_KEY`（32 字节随机密钥的 Base64）。不能把测试密钥或真实凭据写进公开仓库。

详细文档：[实验与会话](docs/backend-experiments.md) · [检索与压缩](docs/backend-context.md) · [数据网关](docs/backend-data.md)。

## 架构与目录

```mermaid
flowchart LR
    UI["React Web"] --> CP["Spring Boot 控制平面"]
    NATIVE["共享 React / Wails"] --> BRIDGE["Go API bridge / 事件轮询"] --> CP
    CP --> MQ["Artemis"]
    MQ --> RUN["Go Runner"]
    RUN --> WS["隔离 Git worktree"]
    RUN --> MODEL["OpenAI-compatible API"]
    RUN --> CONTEXT["私有版本索引 / 原文存储"]
    RUN --> CP
    CP --> STORE["PostgreSQL / Redis"]
```

| 目录 | 内容 |
| --- | --- |
| `agent-engine/` | Go Agent、Runner、工具、工作树、浏览器与 MCP |
| `control-plane/` | Java 控制平面、任务状态、批准与可靠派发 |
| `frontend/` | 共享 React 界面与 Web 应用 |
| `desktop/` | Wails 原生工程、API 代理、目录注册、快照与补丁应用 |
| `protocol/` | 版本化消息和事件契约 |
| `compose*.yml` | 容器启动、测试与联调配置 |
| `docs/` | 架构、安全、验收及各分支专项文档 |

[架构说明](docs/architecture.md) · [安全模型](docs/security.md) · [验收目标](docs/acceptance.md)。验收文档中的性能目标是开发目标，不是已测量的基准结果。

## 开发验证

分别运行三个检查，避免一个容器先退出后中止其他检查：

```powershell
docker compose -f compose.test.yml run --rm agent-test
docker compose -f compose.test.yml run --rm control-test
docker compose -f compose.test.yml run --rm frontend-test
```

单元与契约检查不需要模型 key。当前分支包含 GitHub Actions 的 Go/Java/前端检查和六组服务联调工作流。真实 PostgreSQL/Redis 适配器测试需要单独的可销毁实例，配置见数据文档。

`compose.smoke.yml` 提供模拟流式接口的端到端联调：创建测试源码、运行真实测试并产出检查点。它验证服务接线，不评价模型能力。真实任务仍使用配置的生成接口。
