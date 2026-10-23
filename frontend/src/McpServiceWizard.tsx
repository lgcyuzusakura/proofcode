import { useEffect, useRef, useState } from "react";
import { ArrowDownToLine, FolderPlus, Loader2 } from "lucide-react";
import { desktopApp } from "./api";
import type { BundledRuntime, McpServiceScaffold } from "./types";

export type McpScaffoldProps = { busy: boolean; onBusy: (value: boolean) => void; onCreated: (value: McpServiceScaffold) => void };

export function McpServiceWizard({ busy, onBusy, onCreated }: McpScaffoldProps) {
 const [bundle, setBundle] = useState<BundledRuntime | null>(null), [loaded, setLoaded] = useState(false), [error, setError] = useState("");
 const [name, setName] = useState("my_tools"), [transport, setTransport] = useState<"http" | "stdio">("http"), [port, setPort] = useState("3190"), [docker, setDocker] = useState(false);
 const [creating, setCreating] = useState(false), [result, setResult] = useState<McpServiceScaffold | null>(null), [imported, setImported] = useState(false), [notice, setNotice] = useState("");
 const mounted = useRef(false), pending = useRef(false);
 const supported = !!desktopApp()?.GetMcpServiceTemplate && !!desktopApp()?.CreateMcpService;
 useEffect(() => {
  mounted.current = true;
  if (supported) void desktopApp()!.GetMcpServiceTemplate!().then(value => { if (mounted.current) setBundle(value); }).catch(() => { if (mounted.current) setError("内置工具模板无法读取，可使用下方命令创建。"); }).finally(() => { if (mounted.current) setLoaded(true); });
  return () => { mounted.current = false; };
 }, []);
 if (!supported) return null;
 const valid = /^[A-Za-z][A-Za-z0-9_-]{0,31}$/.test(name) && (transport === "stdio" || /^\d+$/.test(port) && Number(port) >= 1024 && Number(port) <= 65535);
 const create = async () => {
  if (pending.current || busy || !valid || !bundle?.available) return;
  pending.current = true; setCreating(true); onBusy(true); setError(""); setNotice("");
  try {
   const value = await desktopApp()!.CreateMcpService!(name, transport, transport === "http" ? Number(port) : 0, transport === "http" && docker);
   if (!mounted.current) return;
   if (!value.created) { setNotice("未创建工具服务，原配置保持不变。"); return; }
   setResult(value); setImported(false); setNotice("工具文件已创建，尚未安装依赖或启动服务。");
  } catch { if (mounted.current) setError("工具服务创建失败，请检查保存位置，或更换别名；已有文件不会覆盖。"); }
  finally { pending.current = false; onBusy(false); if (mounted.current) setCreating(false); }
 };
 return <section className="mcp-service-wizard" aria-label="桌面创建工具服务" aria-busy={creating}>
  {!loaded ? <p role="status">正在读取内置工具模板…</p> : !bundle?.available ? <p>此开发版本没有可用的内置模板，请使用下方命令创建独立工具目录。</p> : <>
   <h3>在本机创建工具服务</h3><p>选择保存位置即可创建独立目录。运行时需要 Node.js 22 或更新版本；创建不会下载依赖或执行工具。</p>
   <fieldset disabled={busy || creating}>
    <div className="mcp-scaffold-fields"><label>工具服务别名<input aria-label="新工具服务别名" maxLength={32} value={name} onChange={event => setName(event.target.value)} spellCheck={false}/></label><label>传输方式<select aria-label="新工具传输方式" value={transport} onChange={event => { setTransport(event.target.value as "http" | "stdio"); setDocker(false); }}><option value="http">Streamable HTTP</option><option value="stdio">本地 stdio</option></select></label></div>
    <small>别名以字母开头，最多 32 字符，可含数字、下划线和连字符。HTTP 端口为 1024–65535。</small>
    {transport === "http" ? <><label>监听端口<input aria-label="新工具监听端口" type="text" inputMode="numeric" maxLength={5} value={port} onChange={event => setPort(event.target.value)}/></label><label className="agent-platform-check"><input type="checkbox" checked={docker} onChange={event => setDocker(event.target.checked)}/>供 Docker 中的后端访问</label><p>{docker ? "将监听所有网卡，并允许 host.docker.internal。接入前需由管理员启用内网 MCP；HTTP 连接仍需令牌。" : "仅监听 127.0.0.1，适合直接运行在本机的控制面和 Runner。"}</p></> : <p>stdio 需要管理员把生成目录配置到 Runner 的 MCP 服务目录；不会自动在本机运行程序。</p>}
    <button type="button" className="secondary-button" disabled={!valid} onClick={() => void create()}>{creating ? <Loader2 size={15}/> : <FolderPlus size={15}/>}选择位置并创建工具服务</button>
   </fieldset>
  </>}
  {notice && <p role="status">{notice}</p>}{error && <p role="alert">{error}</p>}
  {result && <div className="mcp-scaffold-result"><strong>{result.name}</strong><p className="mcp-scaffold-path">{result.directory}</p><pre aria-label="启动新工具服务命令"><code>{result.commands}</code></pre><p>编辑 <code>tools/custom.mjs</code> 添加工具，把允许检索的文档放入 <code>knowledge/</code>。启动后，再保存配置并诊断连接。</p>
   {result.transport === "http" ? <><p>连接地址：<code>{result.url}</code>。访问令牌保存在新目录的 <code>.env</code>，不会写入浏览器存储。</p><button type="button" className="secondary-button" disabled={busy || imported} onClick={() => { onCreated(result); setResult({ ...result, bearer: "" }); setImported(true); }}><ArrowDownToLine size={15}/>{imported ? "已填入新服务表单" : "填入新服务表单"}</button>{imported && <p>名称、地址和令牌已填入下方草稿，点击“保存服务”后才会发送配置。</p>}</> : <p>管理员请参考生成目录 README 的 stdio 接入说明；该服务没有自动添加到项目。</p>}
  </div>}
 </section>;
}
