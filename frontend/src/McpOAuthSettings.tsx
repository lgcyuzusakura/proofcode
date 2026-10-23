import { useEffect, useRef, useState } from "react";
import { CheckCircle2, CircleAlert, ExternalLink, KeyRound, LoaderCircle, RotateCw, Save, X } from "lucide-react";
import { api } from "./api";
import "./mcp-oauth.css";

type OAuthStatus = { configured: boolean; clientId: string; scope: string; hasClientSecret: boolean; redirectUri: string; state: string; serverVersion: number };
const labels: Record<string, string> = { NOT_CONNECTED: "尚未连接", PENDING: "等待浏览器授权", EXCHANGING: "正在完成连接", AUTHORIZED: "已连接", EXPIRED: "授权已过期", ERROR: "连接失败，请重新授权", CANCELLED: "已取消授权" };

export function McpOAuthSettings({ path, version, disabled, onVersion, onBusy }: { path: string; version: number; disabled: boolean; onVersion: (version: number) => void; onBusy: (busy: boolean) => void }) {
 const [expanded, setExpanded] = useState(false), [status, setStatus] = useState<OAuthStatus | null>(null), [error, setError] = useState("");
 const [clientId, setClientId] = useState(""), [scope, setScope] = useState(""), [secret, setSecret] = useState(""), [clearSecret, setClearSecret] = useState(false), [operation, setOperation] = useState("");
 const [authorizationUrl, setAuthorizationUrl] = useState("");
 const generation = useRef(0);
 const dirty = !status || clientId !== status.clientId || scope !== status.scope || !!secret || clearSecret;
 const waiting = status?.state === "PENDING" || status?.state === "EXCHANGING";
 const endpoint = `${path}/oauth?version=${version}`;
 const adopt = (value: OAuthStatus) => { if (typeof value.configured !== "boolean" || typeof value.clientId !== "string" || typeof value.serverVersion !== "number") throw new Error("OAuth 状态不可用，请重新加载。"); setStatus(value); setClientId(value.clientId); setScope(value.scope); setSecret(""); setClearSecret(false); };

 useEffect(() => {
  const current = ++generation.current;
  if (!expanded) return;
  setError(""); setStatus(null); setAuthorizationUrl("");
  void api<OAuthStatus>(endpoint).then(value => { if (current === generation.current) adopt(value); }).catch(cause => { if (current === generation.current) setError(String(cause)); });
  return () => { generation.current++; };
 }, [endpoint, expanded]);

 useEffect(() => {
  if (!expanded || !waiting) return;
  let stopped = false, timer: ReturnType<typeof setTimeout>;
  const poll = async () => {
   try { const value = await api<OAuthStatus>(endpoint); if (stopped) return; setStatus(value); if (value.state === "PENDING" || value.state === "EXCHANGING") timer = setTimeout(() => void poll(), 2000); else setAuthorizationUrl(""); }
   catch (cause) { if (!stopped) setError(String(cause)); }
  };
  timer = setTimeout(() => void poll(), 2000);
  return () => { stopped = true; clearTimeout(timer); };
 }, [expanded, waiting, endpoint]);

 const save = async () => {
  if (disabled || !status || !dirty) return;
  onBusy(true); setOperation("save"); setError(""); setAuthorizationUrl("");
  try { const value = await api<OAuthStatus>(endpoint, { method: "PUT", body: JSON.stringify({ clientId, scope, clientSecret: secret, clearClientSecret: clearSecret }) }); adopt(value); onVersion(value.serverVersion); }
  catch (cause) { setError(String(cause)); } finally { onBusy(false); setOperation(""); }
 };
 const begin = async () => {
  if (disabled || dirty || !status?.configured) return;
  const popup = window.open("", "_blank", "popup,width=720,height=820");
  if (popup) popup.opener = null;
  onBusy(true); setOperation("begin"); setError("");
  try {
   const value = await api<{ authorizationUrl: string }>(`${path}/oauth/begin?version=${version}`, { method: "POST" });
   const target = new URL(value.authorizationUrl);
   if (target.protocol !== "https:" && !(target.protocol === "http:" && ["localhost", "127.0.0.1", "[::1]"].includes(target.hostname))) throw new Error("授权地址无效，请检查服务端 OAuth 配置。");
   setAuthorizationUrl(target.href); setStatus({ ...status, state: "PENDING" });
   if (popup) popup.location.href = target.href;
  } catch (cause) { popup?.close(); setError(String(cause)); } finally { onBusy(false); setOperation(""); }
 };
 const cancel = async () => {
  onBusy(true); setOperation("cancel"); setError("");
  try { adopt(await api<OAuthStatus>(`${path}/oauth/cancel?version=${version}`, { method: "POST" })); setAuthorizationUrl(""); }
  catch (cause) { setError(String(cause)); } finally { onBusy(false); setOperation(""); }
 };

 return <details className="mcp-oauth-settings" open={expanded} onToggle={event => setExpanded(event.currentTarget.open)}>
  <summary><KeyRound size={15}/><strong>OAuth 登录</strong>{status && <span>{labels[status.state] || "尚未连接"}</span>}</summary>
  <section aria-label="MCP OAuth 设置">
   {!status && !error && <p role="status">正在读取 OAuth 配置…</p>}
   {status && <>
    <label>OAuth 客户端 ID<input maxLength={512} autoComplete="off" value={clientId} onChange={event => setClientId(event.target.value)} placeholder="授权服务分配的客户端 ID"/></label>
    <label>OAuth 客户端密钥（可选）<input type="password" autoComplete="new-password" maxLength={4096} disabled={clearSecret} value={secret} onChange={event => setSecret(event.target.value)} placeholder={status.hasClientSecret ? "已保存，留空保留" : "公共客户端可留空"}/></label>
    {status.hasClientSecret && <label className="agent-platform-check"><input type="checkbox" checked={clearSecret} onChange={event => { setClearSecret(event.target.checked); setSecret(""); }}/>清除 OAuth 客户端密钥</label>}
    <label>OAuth 权限范围（可选）<input maxLength={1024} autoComplete="off" value={scope} onChange={event => setScope(event.target.value)} placeholder="例如：docs.read offline_access"/></label>
    <label>OAuth 回调地址<input readOnly value={status.redirectUri} aria-label="OAuth 回调地址"/></label>
    <div className={`mcp-oauth-status${status.state === "AUTHORIZED" ? " is-connected" : ""}`} role="status">{waiting ? <LoaderCircle size={16} className="mcp-oauth-spinner"/> : status.state === "AUTHORIZED" ? <CheckCircle2 size={16}/> : <KeyRound size={16}/>}<span>{labels[status.state] || "尚未连接"}</span></div>
    <div className="mcp-oauth-actions"><button type="button" disabled={disabled || !dirty} onClick={() => void save()}><Save size={14}/>{operation === "save" ? "保存中…" : "保存 OAuth 配置"}</button><button type="button" disabled={disabled || dirty || !status.configured || waiting} onClick={() => void begin()}>{status.state === "AUTHORIZED" ? <RotateCw size={14}/> : <ExternalLink size={14}/>}连接 OAuth</button>{waiting && <button type="button" disabled={disabled} onClick={() => void cancel()}><X size={14}/>取消授权</button>}</div>
    {authorizationUrl && <a href={authorizationUrl} target="_blank" rel="noopener noreferrer" className="mcp-oauth-link"><ExternalLink size={14}/>打开授权页面</a>}
   </>}
   {error && <p role="alert" className="agent-platform-error"><CircleAlert size={14}/>{error}</p>}
  </section>
 </details>;
}
