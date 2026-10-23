import { Code2 } from "lucide-react";
import "./mcp-service-guide.css";
import { McpServiceWizard, type McpScaffoldProps } from "./McpServiceWizard";

const createCommands = `npm --prefix mcp-service ci
npm --prefix mcp-service run init -- --directory "$env:USERPROFILE\\my-mcp-tools" --name my_tools
Set-Location "$env:USERPROFILE\\my-mcp-tools"
npm ci
npm run check
npm start`;

export function McpServiceGuide(props: McpScaffoldProps) {
 return <details className="mcp-service-guide">
  <summary><Code2 size={16}/><span>创建自己的工具服务</span><span className="mcp-service-guide-hint">接入指南</span></summary>
  <section aria-label="自定义工具接入指南">
   <McpServiceWizard {...props}/>
   <p>把内部 API、知识检索或工作流程做成工具，再分配给自己的智能体。源码附带可独立运行的模板，需要 Node.js 22 或更新版本。</p>
   <ol>
    <li><strong>生成并启动</strong><p>在 ProofCode 源码根目录打开 PowerShell，执行以下命令。目标目录需要尚不存在。</p><pre aria-label="创建 MCP 服务命令"><code>{createCommands}</code></pre><p>新目录包含配置、工具示例和 README；访问令牌保存在其中的 <code>.env</code>，请妥善保管。</p></li>
    <li><strong>添加你的工具</strong><p>编辑 <code>tools/custom.mjs</code>，定义名称、用途、输入参数和 <code>execute</code> 函数；在 <code>config.local.json</code> 的 <code>modules</code> 中添加其他文件。模板自带 <code>project_brief</code> 和 <code>knowledge_search</code>，可先直接体验。</p></li>
    <li><strong>连接到此项目</strong><p>服务别名填 <code>my_tools</code>，服务地址填从控制面和 Runner 可访问的 <code>/mcp</code> 地址，访问令牌填新目录 <code>.env</code> 中的值。保存后诊断，再到“智能体”勾选这项服务。</p></li>
   </ol>
   <p className="mcp-service-guide-network">默认地址是 <code>http://127.0.0.1:3190/mcp</code>。Docker 中的 localhost 指容器本身；连接 Windows 本机服务时通常需使用 <code>host.docker.internal</code>，调整服务监听和主机白名单，并由管理员启用内网连接。具体配置见生成目录中的 README。</p>
   <p>自定义模块运行在服务拥有者的系统权限下。新代码任务使用所选工具，实际调用仍需审批；只读规划不连接外部服务。</p>
  </section>
 </details>;
}
