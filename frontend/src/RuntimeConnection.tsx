import { useEffect, useRef, useState } from "react";
import { clearSession, desktopApp } from "./api";
import type { RuntimeConnection as Connection } from "./types";

export function RuntimeConnection({ disabled, onSaved }: { disabled: boolean; onSaved: () => void }) {
  const [connection, setConnection] = useState<Connection | null>(null), [baseUrl, setBaseUrl] = useState("");
  const [error, setError] = useState(""), [notice, setNotice] = useState(""), [busy, setBusy] = useState(false), [loaded, setLoaded] = useState(false);
  const pending = useRef(false), mounted = useRef(false), dirty = useRef(false), addressInput = useRef<HTMLInputElement>(null);
  const supported = !!desktopApp()?.GetRuntimeConnection && !!desktopApp()?.SaveRuntimeConnection;
  const load = async () => {
    if (pending.current) return;
    pending.current = true; setBusy(true); setError("");
    try {
      const value = await desktopApp()?.GetRuntimeConnection?.();
      if (value && mounted.current) { setConnection(value); if (!dirty.current) setBaseUrl(value.baseUrl); setLoaded(true); }
    } catch { if (mounted.current) { setError("无法读取已保存的后端地址，可以填写地址重新保存。"); setLoaded(true); } }
    finally { pending.current = false; if (mounted.current) setBusy(false); }
  };
  useEffect(() => { mounted.current = true; if (supported) void load(); return () => { mounted.current = false; }; }, []);
  if (!supported) return null;
  const save = async (event: React.FormEvent) => {
    event.preventDefault(); if (pending.current || disabled || !loaded) return;
    setError(""); setNotice("");
    let canonical = "";
    try {
      const parsed = new URL(baseUrl.trim());
      if (!["http:", "https:"].includes(parsed.protocol) || parsed.username || parsed.password || (parsed.pathname !== "" && parsed.pathname !== "/") || parsed.search || parsed.hash || /[?#\\\r\n\t]/.test(baseUrl)) throw Error();
      canonical = parsed.origin;
    } catch { setError("请输入 http 或 https 根地址，不含账号、路径和查询参数。"); addressInput.current?.focus(); return; }
    pending.current = true; setBusy(true);
    // Remove the old session before the native process can switch destinations.
    // Preserve project drafts; only the selected remote navigation is reset.
    const changed = canonical !== connection?.baseUrl;
    if (changed) { localStorage.removeItem("proofcode.selected-scope"); clearSession(); }
    try {
      const value = await desktopApp()!.SaveRuntimeConnection!(canonical);
      if (mounted.current) { setConnection(value); setBaseUrl(value.baseUrl); dirty.current = false; setNotice("后端地址已保存。请重新检查连接，并登录该后端的账户。"); onSaved(); }
    } catch { if (mounted.current) setError("未能保存后端地址，请等待服务操作结束后重试；输入已保留。"); }
    finally { pending.current = false; if (mounted.current) setBusy(false); }
  };
  const startLocal = async () => {
    const start = desktopApp()?.StartStandaloneSession;
    if (!start || pending.current || disabled) return;
    pending.current = true; setBusy(true); setError("");
    try {
      const session = await start();
      localStorage.removeItem("proofcode.selected-scope");
      localStorage.setItem("proofcode.session", session.token);
      window.dispatchEvent(new Event("proofcode:auth-changed"));
      const current = await desktopApp()?.GetRuntimeConnection?.();
      if (mounted.current && current) { setConnection(current); setBaseUrl(current.baseUrl); setNotice("本机独立服务已就绪。"); onSaved(); }
    } catch (cause) { if (mounted.current) setError(String(cause)); }
    finally { pending.current = false; if (mounted.current) setBusy(false); }
  };
  return <form className="runtime-connection" aria-label="后端连接" onSubmit={event => void save(event)}>
    <h2>{connection?.source === "standalone" ? "本机独立运行" : "后端连接"}</h2><p>{connection?.source === "standalone" ? "本机数据保存在当前 Windows 账户下。内置服务随应用启动；填写团队服务地址可切换运行方式。" : "填写 ProofCode 后端地址。模型的 API 地址与密钥在登录后的 Agent 设置中配置。"}</p>
    {desktopApp()?.StartStandaloneSession && connection?.source !== "standalone" && <button className="secondary-button" type="button" disabled={busy || disabled || !loaded} onClick={() => void startLocal()}>启用本机独立运行</button>}
    <label htmlFor="runtime-backend-url">服务地址</label><div className="runtime-connection-controls"><input ref={addressInput} id="runtime-backend-url" type="text" inputMode="url" maxLength={2048} value={baseUrl} placeholder="http://127.0.0.1:8080" disabled={busy || disabled || !loaded} onChange={event => { dirty.current = true; setBaseUrl(event.target.value); setNotice(""); }}/><button className="secondary-button" type="submit" disabled={busy || disabled || !loaded || !baseUrl.trim()}>{busy ? "处理中…" : "保存后端地址"}</button></div>
    {connection && <small>当前来源：{connection.source === "standalone" ? "内置服务" : connection.source === "saved" ? "本机保存" : connection.source === "environment" ? "启动环境" : "默认地址"}。更换地址会退出当前账户，工程草稿仍保留。</small>}
    {error && <p role="alert">{error}</p>}{notice && <p role="status">{notice}</p>}
  </form>;
}
