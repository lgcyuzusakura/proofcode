# 对话与开发工作台

实现分支：`codex/structured-merge-debug-tools`。默认入口是共享 Web/Wails 对话页面，代码、浏览器、Git、代码审查和数据库/缓存各有独立页面。代码任务的助手消息保留一个小型“查看修改”按钮，选择消息绑定的 task ID，再进入该回合的审查页。

## 日常操作

1. 在 Windows 桌面端直接开始新对话，或导入现有工程根目录。直接发送会创建真实桌面的 `ProofCode-Projects` 工程目录。
2. 普通聊天只调用模型；代码任务捕获当前允许的源码，在 Runner 的隔离工作树执行。模型输出支持 Markdown、表格、代码复制及实时文字；Enter 发送，Shift+Enter 换行，中文输入法组合输入不触发发送。
3. 打开“代码 IDE”，搜索/打开文件，使用 Monaco 编辑和 Ctrl+S 保存。新增文件通过相对路径创建。切页面或工程后，同一工程的未保存草稿可以从当前 WebView/浏览器会话恢复；关闭会话不保证恢复，保存才写入磁盘。
4. 命令控制台使用“程序 + JSON 参数数组”，工作目录固定为注册工程目录。它是用户主动执行的本机程序，拥有当前用户的权限。Windows npm/npx 的 `.cmd` 入口转换为 Node 执行其 JavaScript 入口，参数不会拼入 shell。每工程同时运行一个程序，输出最多 1 MiB，可以停止进程树。
5. 打开“浏览器”并输入开发地址。原生端以临时配置启动独立 Edge/Chrome，展示真实的 1280×800 页面截图。可以点击、输入、Enter、滚动、后退/前进/刷新，查看页面文本和控制台。“打开完整 Chromium DevTools”在独立临时窗口打开 Chromium 自带 inspector，连接当前项目页面。关闭项目浏览器会清理检查器；截图预览仍不是视频视口。
6. 打开“Git”，精确暂存/取消暂存文件，提交、创建/切换本地分支、设置 origin、获取远程、仅快进拉取或推送。设置远程、拉取、推送有明确确认；不支持强推。结构化合并在隔离仓库中运行 Git ort 和可选 Mergiraf，展示三方原文、结果和 diff；必须审查批准后才能应用。远程支持无密码的 HTTPS URL 或 `ssh://git@host/path`。Git 身份及凭据由本机 Git 配置管理。
7. 在代码任务消息旁点击“查看修改”，审阅 checkpoint/recovery 补丁，处理工具审批，查看验证/执行证据，下载 patch，或批准应用到绑定的本机工程。审批详情不占据主聊天页面。审查笔记保存在当前设备的 localStorage。
8. 在“代码 IDE”启动标准 stdio LSP 或 DAP 服务。语言服务接入补全、悬停、F12 定义和诊断；调试器接入 launch/attach、断点、调用堆栈、作用域、变量、表达式求值、继续和单步。点击行号左侧设置断点，补全也有图标入口。每项目每种协议只允许一个活动服务，离开 IDE 会停止服务。

## 文件与 Git 一致性

编辑保存核对打开时的 SHA-256；文件被外部修改或删除时拒绝覆盖，并保留草稿。Windows 保存使用独占文件句柄；新文件使用 CREATE_NEW。`.proofcode/editor-history` 记录保存前内容、目标哈希及 PREPARED/SAVED/CONFLICT 状态，供恢复与排查；当前没有自动编辑撤销界面。

Git 操作先核对状态指纹，包含文件清单、工作区/暂存 diff、HEAD、index entries、分支及 origin。受保护路径不能从此界面暂存或提交。注册路径必须是仓库根，嵌套目录不能操作父仓库 index。新初始化仓库在本机 `.git/info/exclude` 排除 ProofCode 的内部记录。Git 检查无法阻止其他程序在检查与命令之间改变仓库，操作后应检查刷新后的实际状态。

原有快照限制仍适用：每文件 1 MiB、总计 8 MiB、最多 2000 文件，跳过凭据、生成目录、链接和子模块。IDE 当前使用同一受控文件集合。Web 端只能读取授权作用域内任务的不可变源码快照；原生编辑、命令、Git和浏览器需要绑定本机目录。远程仓库未提供源码快照时，Web IDE明确显示无可读取文件。

任务工作树将新生成的 `.proofcode`、`.context-store`、Python 测试缓存、node_modules 和 `.venv` 加入仓库本机 exclude；已跟踪文件的修改仍进入补丁。这避免把测试缓存或截图二进制当成源码修改。浏览器工具的截图保存在 Runner 工作树中，当前不是控制平面可下载的独立图片 artifact。

## Agent 浏览器工具

桌面手动浏览器与 Runner 的任务浏览器是两个独立会话。手动浏览器不暴露 Wails 方法给访问的网页，也不使用用户的日常登录配置。

普通 CODE 任务可通过 `RUNNER_BROWSER_ENABLED=true` 开启 `browser` 工具；镜像提供 Chromium，默认路径 `/usr/bin/chromium-browser`。首次实际调用才启动浏览器，任务结束、取消或暂停后关闭。CHAT 和 A–F 对照任务不添加此工具，保持实验配置固定。

标准 Docker会拒绝 Chromium的用户 namespace沙箱。Compose通过显式的 `RUNNER_BROWSER_NO_SANDBOX=true` 使用非 root Runner容器作为这一部署模式的隔离边界；没有提升容器权限。在支持 Chromium沙箱的主机上设置 false。Windows手动浏览器不使用此开关。

工具支持 `open / snapshot / click / type / console / screenshot`。open/click/type 为 EXEC 风险，沿用工具批准流程；读取 DOM、控制台和保存任务内部截图为只读验证操作。URL 只接受有主机的 HTTP/HTTPS，拒绝嵌入用户名密码。调用结束不关闭会话，调用取消会中断执行。浏览器进程不能替代任务容器的安全隔离；访问本机地址或远程网站具有网络副作用。

## 验证与参考

原生单元/集成测试：`desktop/native/developer_test.go`。包含编辑冲突、越界、审计文件、Git状态版本、精确暂存、分支、命令/子进程停止、真实 Edge 点击/输入/截图与工程隔离。

可选 UI 联调：在共享前端 Vite与隔离控制平面启动后，设置 `PROOFCODE_UI_SMOKE=1`、`PROOFCODE_PLAYWRIGHT_MODULE=<Playwright module>`，在 native目录执行 `go test -run TestDeveloperUIEndToEnd -count=1 -v`。测试使用临时工程，通过临时 loopback测试传输调用真实 App方法；生产 Wails绑定由独立 production build验证。脚本 `smoke/developer/ui.cjs` 使用确定性模型夹具；结果属于工程验证，不属于论文模型性能数据。

参考及依赖：

- [Monaco Editor](https://github.com/microsoft/monaco-editor)：内置代码编辑器、语言着色、TypeScript/JSON worker。
- [chromedp](https://github.com/chromedp/chromedp)：真实 Edge/Chrome/Chromium浏览器控制。
- [Wails](https://github.com/wailsapp/wails)：Windows WebView2与Go原生桥接。
- [react-markdown](https://github.com/remarkjs/react-markdown) / [remark-gfm](https://github.com/remarkjs/remark-gfm)：Markdown与表格；禁用原始HTML，远程图片按说明文字显示。
- [Git](https://git-scm.com/docs)：porcelain状态、index、分支、diff及远程操作。

这些项目是依赖与实现参考；不声称复制了 Codex/Claude Code 全部能力。已新增标准 stdio LSP/DAP、审批式结构化合并和原生 Chromium DevTools，详见[研究与实现说明](structured-merge-and-debugging.md)。当前没有扩展市场、Git rebase/stash、LSP rename/format/workspace edit 完整支持或远程 TCP 调试适配器。
