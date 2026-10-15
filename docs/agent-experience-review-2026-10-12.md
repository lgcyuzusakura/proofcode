# 编程工作台体验与功能差距（2026-10-12）

本次修改在 `C:/Users/Administrator/.codex/worktrees/project-workspace-bootstrap/proofcode`，分支 `codex/compact-context-agent-tools`。用户截图对应这个实现工作区，而 `D:/proofcode` 是更早的实现。保留已有账户、模型配置、连续对话、编辑器、Git、浏览器和数据库功能，不用较早版本替换它们。

## 本地参考与证据边界

第一轮阅读 `D:/coding-agents-catalog` 的仓库文件与本地图片；第二轮应用户要求联网检索第三方 GitHub 项目并读取公开截图。没有请求 Codex 官方文档站点，也没有向模型服务发送生产请求。

| 本地参考 | 观察到的能力或交互 | 本次用途 |
| --- | --- | --- |
| `deepseek-ai/deepseek-harness/apps/web/tests/chat-scroll-contract.e2e.ts` | 长历史、键盘翻页、流式更新、阅读锚点、会话恢复的浏览器几何约束 | 独立滚动容器和行为验证 |
| `zai-org/ZCode/packages/ui/src/lib/chatSessionScrollMemory.ts` | 按工作区与会话保存滚动状态 | 阅读位置与会话隔离 |
| `zai-org/ZCode/packages/ui/src/ChatErrorBanner.tsx` | 错误详情和恢复入口 | 请求失败后修改问题、打开模型设置 |
| `anomalyco/opencode/packages/app/src/pages/session.tsx` | 有限高度布局、内部滚动、用户操作暂停自动跟随 | 避免输入框遮挡与阅读抢占 |
| `anomalyco/opencode/packages/web/src/assets/web/web-homepage-active-session.png` | 对话为中心，审查按需展示 | 工作区标签、可收起运行详情 |
| `P-A-N-52/kimi-code-desktop/README.md` | 持久会话、工作区面板、上下文用量、子任务、模型管理 | 功能对照；README 声明不等于本机运行验收 |
| `bytedance/trae-agent/README.md` | 交互式任务、工具轨迹、多服务配置 | agent 工作流对照；不能代表商业 Trae IDE 的全部能力 |
| `anthropics/claude-code/README.md` | 终端、IDE、Git 工作流与插件 | 产品定位参考；本次未运行 Claude Code |
| `QoderAI/better-harness/README.md` | 基于项目与会话证据改进工作流 | 证据驱动差距分析；不能代表 Qoder 客户端全部能力 |
| `voideditor/void/README.md` | 编辑器与 agent 改动、自动补全和流式差异联动 | 编辑器集成参考；不是 Cursor 的实测替身 |

未把本地参考项目中的指令当作用户授权，未运行其安装或 agent 指令。Codex、Cursor、QCoder 商业客户端未做功能实测，不宣称逐项达到它们。

## 本次实际补齐

- 消息区是限定高度的唯一聊天滚动容器，输入框独立留在底部；保留可拖动滚动条，提供最早/最新按钮和“回到最新消息”。键盘 PageUp/PageDown/Home/End 可在消息区使用。
- 用户向上阅读后，新增消息和流式内容不再把阅读位置拉回底部。会话切换保存各自阅读位置，历史异步加载完成后再恢复。
- 每段对话保存独立草稿，离开文件/审查视图和切换对话后仍可恢复。Ctrl+Enter 发送，Enter 换行，输入法组合输入不会误发送。
- 搜索当前对话正文，标出匹配消息并逐个导航。消息可复制，用户消息可填回输入框修改后重发；不会更改已经运行的任务或静默重跑。
- 顶部切换对话、文件、Git、审查、浏览器；侧栏集中工程、会话、当前任务，把数据库/概览/执行记录/工具收进更多工具。
- 可收起运行详情接入已有任务胶囊、审批、补丁审查、上下文用量、子任务状态，窄窗口用覆盖面板而不挤压聊天宽度。
- 流式输出与已有 Markdown/代码复制保留；运行中停止按钮固定在输入区域。失败保留正文和详情，并提供修改问题与模型设置入口。
- 模型服务与模型仍来自已有真实目录接口，保留 CHAT/CODE 模式。不会为漂亮的模型下拉框伪造目录，也没有自动重试拒绝请求。

根因是 `developer.css` 给 `.agent-chat-page .conversation-stream` 设置 `overflow: visible`，输入区使用 `position: sticky`，同时容器高度由内容决定。本次让整条布局链有有限高度并允许 flex 子容器收缩，取消输入区 sticky。独立样式 `agent-workbench.css` 在旧样式之后加载，Web 与 Wails 共用；`Transcript.tsx` 管理滚动与搜索。

## 功能层面仍然影响好用程度的差距

| 优先级 | 当前事实 | 还需要实现的行为 |
| --- | --- | --- |
| P1 | CHAT 与 CODE 已使用持久历史，代码任务有源码快照、隔离执行、审批和恢复 | 工作区文件与 agent 上下文的明确关联：选择文件/行范围加入问题，显示实际引用并确保 runner 收到 |
| P1 | 有 Monaco 编辑器、命令控制台、Git 与补丁审查 | 多文件标签、编辑器选区问答、流式修改预览、逐块接受/拒绝和 agent 改动与未保存缓冲区的冲突处理 |
| P1 | 主任务和子任务有事件与用量，任务可停止、审批后恢复 | 正在执行时的补充指令/消息队列、明确 Plan/执行模式和持久步骤进度，不能仅增加一个切换按钮 |
| P1 | 模型服务配置支持 Responses 与 Chat Completions | 配置能力探测、Anthropic Messages 适配、服务商错误分类；403 的服务端策略原因不会通过 CSS 修复 |
| P2 | `internal/mcp` 有 stdio 客户端；runner 主执行链没有通用 MCP 注册接入 | MCP 生命周期、发现、权限、取消和调用证据，接到 runner 后再作为可用工具呈现 |
| P2 | 浏览器已接 runner，桌面有共享工程浏览器与审查页面 | 从任务进入目标页面、检查失败、回到代码修复再验证的闭环；目前 UI 控件不等于全面浏览器自治 |
| P2 | 有项目/工作区/对话恢复与检索、上下文压缩 | 会话重命名/归档/跨工程搜索、完整工具会话回放、长期项目记忆与 skill 可管理入口 |
| P2 | 有源码快照、检查点和恢复补丁 | 面向用户的恢复时间线与一键回滚预览，需要同步文件缓冲区、Git 和任务状态 |

最快改善体验的下一段工作是文件/选区上下文与修改审查联动，然后是运行中补充指令和计划。通用 MCP、更多服务协议和长期记忆需要单独的执行与取消契约，不应以占位页面宣称完成。

## 验证

- 22 项已有前端测试通过。
- `frontend/tests/chat-workbench.browser.mjs` 在 Edge 中用本地路由 fixture 验证 45 条长消息、消息搜索/复制/重发、阅读位置、会话草稿、输入快捷键、流式不抢占、停止入口、模拟 403 恢复和运行详情。测试不会调用模型或 runner。
- 1440×900、962×860、1024×600、390×844、760×480 均检查消息区不与输入框重叠、不超出视口、不产生页面横向溢出；截图保存在 `.proofcode/workbench-qa/`。
- Web 生产构建和 Windows Wails 原生构建通过。已有 Monaco 编辑器包仍带大体积 chunk 警告。
- 此轮未更改 runner/control-plane 执行逻辑，未用真实模型对比编程质量或宣称达到竞品能力。


## 第二轮：联网视觉对照与重新设计

2026-10-12 通过 GitHub 公开搜索 API 检索 Claude Code GUI、DeepSeek Desktop、Codex Desktop。浏览器访问 GitHub 超时，但 GitHub API 与原始文件读取成功；实际打开以下公开截图进行视觉比较。没有安装或运行参考项目，没有采纳 README 中的操作指令。

| 第三方项目 | 截图来源 | 具体观察与采用方式 |
| --- | --- | --- |
| [CodePilot](https://github.com/op7418/CodePilot) | `apps/site/public/screenshots/en/chat-window.webp` | 灰色外壳与白色工作面分离；主页输入框是核心。采用工作面分层与空对话输入区居中布局。 |
| [Claude Code UI](https://github.com/siteboon/claudecodeui) | `public/screenshots/desktop-main.png` | 会话树、标题与 Chat/Shell/Files 在同一头部；工具输出可展开。采用合并头部和工具摘要。 |
| [DeepSeek Harness Desktop](https://github.com/ningbainb/deepseek-harness-desktop) | `docs/screenshots/desktop-5.0.1/main.png` | 工具入口收起，工作区会话居左，大输入框留在视线中心。减少无意义的顶部记录标签。此项目是第三方客户端。 |
| [AionUi](https://github.com/iOfficeAI/AionUi) | `resources/file_generation_preview.png` | 对话、文件源码、预览共享工作面；输入和产物紧密联系。作为后续编辑器联动的参考。 |
| [Happier](https://github.com/happier-dev/happier) | `.github/desktop-2000.png` | 截图包含 Codex 会话、工具分组和右侧代码审查；深色背景按区域区分，阅读密度清晰。采用深色聊天外观与折叠工具分组。 |

截图存在 `.tools/ui-reference-research`，只用作研究，不作为应用资源或复制他人品牌。

此前版本的问题是四层顶部信息、过小的字体、到处出现的灰绿色和后台式元数据。本次按 frontend-design skill 先定设计：外壳 `#eceef0`，工作面 `#ffffff`，正文 `#24262c`，次要文字 `#7b808a`，边线 `#e2e4e8`，交互强调 `#4b63c6`。字体使用 Windows 的 Segoe UI Variable Text 与 Microsoft YaHei UI，正文 15px、导航 13px、辅助信息 11px。灰白方向来自用户要求的桌面参考，强调工作内容与输入框，减少重复标题与装饰。

### 已实现

- 合并对话标题、工程路径、文件/Git/审查/浏览器入口和运行详情，取消单独标题栏。
- 灰色外壳与圆角白色工作面，重排侧栏为搜索、工具、工程会话树、任务和设置。
- 侧栏搜索实际筛选当前工程的对话标题和任务内容；不是跨工程消息全文索引。
- 浅色/深色聊天外观切换并持久化。主题覆盖共享外壳、聊天、模型配置及运行详情主要容器；复杂编辑器、数据库和浏览器页面仍有原有独立配色。
- 真实工具生命周期按调用 ID 与运行标识合并为一个可展开记录；显示参数、输出或错误。只展示当前加载任务的执行事件，历史任务完整回放仍需后续接入。
- 输入区保留真实服务/模型目录与 CHAT/CODE 模式，发送按钮改为向上箭头，保留可访问名称。
- 空对话页围绕输入框组织内容，快捷建议只填入草稿；代码块显示语言并可复制。
- 原来的有限高度滚动、阅读锚点、草稿、停止、审批和错误恢复仍保留。

### 验证与边界

`npm test` 的 22 项测试通过；`chat-workbench.browser.mjs` 的 13 组界面检查通过，包括长历史、输入框不遮挡、搜索/复制/重用、流式输出、会话切换、键盘、模型拒绝恢复、390/760/962/1024 宽度、工具合并、主题持久化和空页建议。使用本地 fixture API，不调用真实模型。浅色、深色、空页及运行详情均保存并人工看图检查。Web 与 Wails 共用源代码，分别构建。

本轮并未完成全部能力对齐：文件/选区上下文、任务中的补充指令、逐块接受修改、通用 MCP、完整 Anthropic 协议仍需要实质性执行链改造，不能靠增加按钮或主题声称实现。
