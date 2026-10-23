# ProofCode 自定义工具服务

这是一套可独立复制、运行的 MCP 服务。把自己的 JavaScript 工具模块放入 `tools/` 并配置名称、用途、JSON Schema 和执行函数，便能接入 ProofCode 的智能体，也可接入其他兼容 MCP 客户端。支持 stdio 与 Streamable HTTP，使用官方 TypeScript SDK。

需要 Node.js 22 或更新版本。工具模块是服务拥有者信任的本地程序，拥有该进程的系统权限；加载配置和 `check` 都会导入模块，不是代码沙箱，也不会代替客户端的工具审批。

## 创建服务

在 ProofCode 源码的 `mcp-service` 目录运行，目标父目录需已存在：

```powershell
npm ci
npm run init -- --directory D:\my-mcp-tools --name my_tools
Set-Location D:\my-mcp-tools
npm ci
npm run check
npm start
```

模板生成一个完整独立目录，包含运行源码、锁定依赖、示例工具和 `knowledge/`。已存在的目标目录会被拒绝。随机 HTTP token 写入新目录的 `.env`，终端不会输出 token；`.env`、`config.local.json` 和知识文件默认被 Git 忽略。新目录无需原 ProofCode 仓库即可运行。`npm run check` 只验证配置及加载工具目录，不调用工具、不监听端口。

默认监听 `http://127.0.0.1:3190/mcp`。配置在 `config.local.json`：

- `root`：工具收到的工作目录；默认 `./knowledge`。
- `modules`：明确列出的本地 `.mjs`/`.js` 文件，路径相对配置文件；不会自动扫描或下载模块。
- `timeoutMs`：单工具异步执行上限，100–60000 毫秒。
- `concurrency`：同时执行的工具数，1–8。忽略取消的未结束操作继续占用额度。
- `secretEnv`：工具可能回传的密钥所在环境变量名；结果文本按值遮蔽。不要在工具元数据、异常、日志中打印密钥。
- HTTP 的 `tokenEnv`：读取 token 的环境变量；HTTP 服务必须设置至少 32 字符 token。
- HTTP 的 `host`/`port`：监听地址和端口；`allowedHosts` 是客户端实际使用的主机名白名单，`allowedOrigins` 是浏览器来源白名单，默认空列表。不读取转发头，不开放跨源浏览器访问。

`.env` 由 `npm start`/`npm run check` 通过 Node 的环境文件参数读取。直接运行 `node src/cli.mjs --config ...` 时应由调用者提供环境变量。

## 接入 ProofCode

打开“智能体与记忆 → MCP 服务”，填 `my_tools`、服务地址和 `.env` 中 `PROOFCODE_MCP_TOKEN` 的值（只填值，填入访问令牌）。保存后主动诊断，查看 `knowledge_search` 与 `project_brief` 的参数目录，然后在自己的智能体中勾选该服务。新代码任务才能发现和使用它，外部调用仍需审批；普通聊天、只读规划和只读智能体不连接服务。

服务地址必须从**控制面及 Runner**可达，不能仅从桌面浏览器可达。Docker 内的 `localhost` 指容器本身：本机 Windows 服务通常需改用 `http://host.docker.internal:3190/mcp`，相应调整监听地址为 `0.0.0.0` 并在 `allowedHosts` 加入 `host.docker.internal`。本机/内网连接还需管理员设置 `PROOFCODE_MCP_ALLOW_PRIVATE_HOSTS=true`。外网部署通过 HTTPS 反向代理，保留认证和 Host 校验，避免把未加密 HTTP 暴露到公网。

已有远程部署可直接使用远程服务地址；无需修改 ProofCode 模型主循环或上传本机可执行文件。保存、打开设置和 `check` 都不自动调用工具。

## 编写工具

生成的 `tools/custom.mjs` 带有可执行的 `project_brief` 示例。每个文件导出数组：

```javascript
export const tools = [{
  name: "my_tool",
  description: "Explain when the agent should use this tool.",
  inputSchema: {
    type: "object",
    properties: { query: { type: "string", minLength: 1, maxLength: 200 } },
    required: ["query"],
    additionalProperties: false
  },
  async execute({ query }, { root, signal }) {
    signal.throwIfAborted();
    // Call your API or use explicitly scoped data; pass signal to fetch/file APIs.
    return { query, items: [] };
  }
}];
```

工具名称最长 24 字符，同一服务不能重复；最多 16 个模块、128 个工具。JSON Schema 严格校验输入；输出 JSON 或文本最多 256 KiB。异常只返回通用错误，HTTP token 及 `secretEnv` 声明的值会从结果文本中遮蔽。stdout 留给 MCP 协议；日志请写 stderr 并自行脱敏。超时/取消依赖异步代码配合 `signal`，无法中断同步死循环或撤销已经提交的外部操作。

HTTP 会话以随机 ID 绑定初始化后的请求，支持跨请求取消通知、HTTP 断开取消和 DELETE 关闭；最多 64 个会话，无活动请求且空闲 10 分钟后清理。不提供后台 GET 通知流。每次请求仍需 Bearer 和主机/来源校验；丢失的会话返回明确错误，客户端需重新初始化。

`knowledge_search` 是随附的本地关键词检索例子：只读取所选目录中的 Markdown/文本，返回路径、行号与片段。它限制目录深度、文件数和体积，不读取链接、隐藏目录、`thesis/`、`backups/` 或 `node_modules/`。这不是向量检索，不应把私有论文或凭据放进工具知识目录。跨机器部署时文档留在服务所在机器，只有查询命中的片段经工具调用返回。

## stdio 接入

初始化时添加 `--transport stdio`，或把配置的 `transport` 改为 `{"type":"stdio"}`。为 ProofCode Runner 管理员配置：

```json
[{"name":"my_tools","command":"node","args":["/opt/my-mcp-tools/src/cli.mjs","--config","/opt/my-mcp-tools/config.local.json"]}]
```

将数组放入 `RUNNER_MCP_SERVERS`，智能体填相同别名。路径和依赖必须存在于 Runner 的执行环境；Windows 路径不能直接用于 Linux 容器。远程用户无权通过服务配置指定控制面要运行的任意程序。自定义进程不是系统隔离沙箱。

## 验证

```powershell
npm test
npm audit --omit=dev --audit-level=moderate
```

Go 客户端互操作测试（在源码 `agent-engine` 目录）：

```powershell
$env:PROOFCODE_CUSTOM_MCP_TEST = "1"
go test ./internal/mcp -run TestCustomServiceInteroperability -count=1 -v
```

测试启动真实 Node 服务与临时本地文件，验证 HTTP/stdio 发现、参数校验、执行结果和密钥遮蔽；不调用模型或外部业务 API。
