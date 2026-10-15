# 紧凑上下文、子任务与共享浏览器

更新日期：2026-10-10。实现分支：`codex/compact-context-agent-tools`。

## 上下文和缓存

主循环默认给每次请求约 16,000 输入 Token、最多 2,048 输出 Token。`PROOFCODE_MODEL_CONTEXT_TOKENS=18304` 包含输出预留和 256 Token 协议估算；`PROOFCODE_OUTPUT_RESERVE_TOKENS=2048` 不能小于主循环输出上限。配置窗口不能超过 128,000。请求发送前还有保守的 UTF-8 字节安全边界；这些是预算估算，未接入 provider 的精确 tokenizer。

工具 JSON、完整 transcript 和用户约束先保存在工作树以外的 context store。大型成功观察返回带引用的头尾预览，旧成功日志可去重和按完整调用/返回协议组移除。全部用户/system 要求、错误、批准/拒绝以及 `data_*` 结果受保护；超预算会明确失败，不偷偷丢失决策。模型可通过 `context_read` 分页读取原始字节。

工具定义按名称稳定排序，初始 system 前缀保持稳定，以便 provider 的前缀缓存工作。不会把本地检索缓存当成模型缓存命中：界面分别展示单次输入、输出、实际缓存输入和主任务累计用量，缺失 usage 显示未报告。缓存命中由服务提供方决定，系统不承诺每次命中。

## 本机配置和模型

共享 `pkg/config` 解析本机 `CODEX_HOME/config.toml` 或用户目录 `.codex/config.toml`，支持选中 profile、自定义 provider 的 `base_url`、`env_key`、`wire_api`，以及本机 API key。`responses` 与 `chat` 都可执行工具协议；OAuth 登录凭据不会冒充 API key。桌面配置和模型目录绑定只返回模型 ID 与脱敏状态，错误不包含服务端响应正文或 TOML 密钥源行。

本机 Runner 在没有显式模型环境配置时读取同一配置；Docker 需要显式环境配置。读取桌面模型目录不等于 Docker 已得到凭据。凭据及浏览器 capability descriptor 存在仓库之外，并有 Git 忽略规则。

## 按需协作

普通代码任务为主 Agent 注册 `spawn_agent`、`list_agents`、`send_message`、`read_agent_messages`、`wait_agent`、`cancel_agent`。子任务只有读取和消息能力，不能再创建子任务。每个主任务最多 4 个子任务、子任务并发 1，每个最多 6 个步骤；主任务可以在子任务调查时继续运行。

每个子任务使用独立历史、约 12k 输入预算、1,024 输出上限和私有 transcript 引用。大文件观察可压缩后回取，不复制完整父历史。工具调用时再次检查实际参数的风险，阻止动态工具切换到写入或执行。消息长度、收件人与任务作用域有校验；重启把未完成子任务标为 interrupted，不自动重放模型调用。主任务和子任务用量分别显示；当前没有统一的费用金额硬预算。

A–F 实验继续遵循固定实验 profile，普通任务的新按需协作不会改变实验分组。

## 共享浏览器

桌面按本地工程维护独立 Chromium 标签页，注册项目 UUID 与不透明本地 handle 后，同用户本机 Runner 可通过 loopback capability 服务操作同一 tab。模型不能选择其他项目、调试地址、凭据或个人浏览器 profile。

桌面提供地址栏、后退/前进、刷新、点击和输入、控制台、页面文本、控件引用、打开同一原生窗口和 DevTools。人工接管会阻止 AI 导航、点击和输入，交还后恢复。AI 浏览器工具提供 open/snapshot/click/type/fill/enter/scroll/back/forward/refresh/console/screenshot，不开放任意 JavaScript。截图作为本地文件，图片不会追加到模型文本上下文；页面文本、标签和控制台输出都有界限，网页内容按不可信数据处理。

本机 Runner 设置 `RUNNER_BROWSER_ENABLED=true`、`RUNNER_BROWSER_MODE=shared`。Docker 默认 `isolated`，不把 localhost 指向的另一容器误当桌面；没有自动的跨容器桌面共享桥。

## 验证记录

2026-10-10 接续开发中，Agent Engine 全套 Go 测试、Windows 原生 Go 测试、前端 18 项测试、前端生产构建和 Wails Windows 生产构建通过。Docker 中 Agent/context 的 race 检测通过。新增测试验证：子任务读取大型文件后保持小请求、能回取原文、其他子任务不能读取其私有引用，动态工具不能执行写入，以及 trailing JSON 参数被拒绝。

真实 Chromium 浏览器测试确认同一 tab、AI 点击对人工视图可见、项目隔离和接管阻止写入。React/native 联调通过桌面与 390px 窄屏验收，页面脚本异常和横向溢出为零；用量和协作卡片数据在 UI 验收中是明确的 fixtures，不能作为真实模型质量证据。

上一轮历史记录报告本机 provider 返回 9 个模型，低成本 smoke 使用 14,242 输入 / 201 输出 Token，其中缓存输入 7,768；文件读取和连接列表成功，PostgreSQL schema 在修正数据会话后通过本地 API 读取。接续本轮没有重复调用真实模型，不新增这项费用。项目没有调用原生生图；界面验收使用浏览器测试。
