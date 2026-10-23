import { useEffect, useRef, useState } from "react";
import { AlertCircle, Check, FolderOpen, Loader2, Play, RefreshCw, Square, Wrench } from "lucide-react";
import { desktopApp } from "./api";
import type { RuntimeManagement, RuntimeReport } from "./types";
import { RuntimeConnection } from "./RuntimeConnection";
import { RuntimeSetup } from "./RuntimeSetup";

const states: Record<string, string> = { ready: "可用", missing: "未安装", unavailable: "不可用", blocked: "未检查", unknown: "未知" };
const phases: Record<string, string> = { preparing: "正在准备本机运行文件…", selecting: "正在检查所选部署目录…", checking: "正在读取服务状态…", starting: "正在构建并启动服务，首次运行可能需要数分钟…", stopping: "正在停止服务…" };
const serviceStates: Record<string, string> = { running: "运行中", exited: "已停止", created: "已创建", restarting: "重启中", paused: "已暂停", removing: "移除中", dead: "异常", not_created: "未创建" };
const healthStates: Record<string, string> = { healthy: "健康", unhealthy: "健康检查失败", starting: "准备中" };
export function RuntimeEnvironment() {
  const [report, setReport] = useState<RuntimeReport | null>(null), [error, setError] = useState(""), [busy, setBusy] = useState(false);
  const [management, setManagement] = useState<RuntimeManagement | null>(null), [managementError, setManagementError] = useState("");
  const pending = useRef(false), managementPending = useRef(false), managementRef = useRef<RuntimeManagement | null>(null), mounted = useRef(false);
  const supported = !!desktopApp()?.CheckRuntime;
  const managed = !!(desktopApp()?.GetRuntimeManagement && desktopApp()?.RefreshRuntimeManagement && desktopApp()?.SelectRuntimeDirectory && desktopApp()?.ManageRuntime);
  const updateManagement = (value: RuntimeManagement) => { if (mounted.current) { managementRef.current = value; setManagement(value); } };
  const readManagement = async () => {
    try {
      const value = await desktopApp()?.GetRuntimeManagement?.();
      if (value && (!managementPending.current || value.busy)) updateManagement(value);
    } catch { if (mounted.current) setManagementError("无法读取本机服务配置，请重新选择部署目录。"); }
  };
  const run = async (refreshServices = false) => {
    if (pending.current || managementPending.current || managementRef.current?.busy) return;
    const check = desktopApp()?.CheckRuntime;
    if (!check) { setError("运行环境检查仅在 Windows 桌面版中提供。"); return; }
    pending.current = true;
    setBusy(true); setError("");
    try { const value = await check(); if (mounted.current) setReport(value); } catch { if (mounted.current) setError("无法读取本机运行环境，请重启桌面应用后重试。"); }
    finally { pending.current = false; if (mounted.current) setBusy(false); }
    if (refreshServices && managed && managementRef.current?.configured) await operate("check");
  };
  const operate = async (action: "select" | "start" | "stop" | "check" | "prepare", write = false, execute = false) => {
    if (managementPending.current || pending.current || managementRef.current?.busy) return;
    const bridge = desktopApp();
    if (!managed || !bridge) return;
    managementPending.current = true; setManagementError("");
    const previous = managementRef.current;
    updateManagement({ directory: "", configured: false, running: false, observed: false, message: "", checkedAt: "", services: [], ...previous, busy: true, phase: action === "prepare" ? "preparing" : action === "select" ? "selecting" : action === "check" ? "checking" : action === "start" ? "starting" : "stopping" });
    try {
      const value = action === "prepare" ? await bridge.PrepareBundledRuntime!(write, execute) : action === "select" ? await bridge.SelectRuntimeDirectory!() : action === "check" ? await bridge.RefreshRuntimeManagement!() : await bridge.ManageRuntime!(action);
      updateManagement(value);
    } catch {
      if (mounted.current) setManagementError("服务操作未完成，请重新检查状态后重试。");
      // Wails rejects errors instead of returning the accompanying Go state.
      try { const value = await bridge.GetRuntimeManagement!(); updateManagement(value); }
      catch { if (previous) updateManagement({ ...previous, busy: false, observed: false, running: false, phase: "error" }); else updateManagement({ directory: "", configured: false, running: false, observed: false, busy: false, phase: "error", message: "", checkedAt: "", services: [] }); }
    } finally { managementPending.current = false; }
  };
  useEffect(() => { mounted.current = true; void run(); if (managed) void readManagement(); return () => { mounted.current = false; }; }, []);
  useEffect(() => {
    if (!management?.busy || !managed) return;
    let pendingRead = false, active = true;
    const timer = window.setInterval(async () => {
      if (pendingRead) return; pendingRead = true;
      try { const value = await desktopApp()?.GetRuntimeManagement?.(); if (active && value && (!managementPending.current || value.busy)) updateManagement(value); }
      catch { /* Keep the active operation visible until the next successful read. */ }
      finally { pendingRead = false; }
    }, 1200);
    return () => { active = false; window.clearInterval(timer); };
  }, [management?.busy, managed]);
  const working = busy || !!management?.busy;
  return <section className="runtime-environment developer-page" aria-busy={working}>
    <div className="developer-heading"><div><h1>运行环境</h1><p>检查桌面端依赖与 ProofCode 服务状态。</p></div><button className="secondary-button" disabled={working || !supported} onClick={() => void run(true)}>{working ? <Loader2 size={14}/> : <RefreshCw size={14}/>}重新检查</button></div>
    <RuntimeConnection disabled={working} onSaved={() => { setReport(null); setError(""); }}/>
    {error && <p className="runtime-environment-note" role={supported ? "alert" : "status"}>{error}{report && " 下方保留上次检查结果，当前状态尚未确认。"}</p>}
    {busy && <p className="runtime-environment-note" role="status"><Loader2 size={15}/>正在检查本机环境…</p>}
    {managed && <section className="runtime-management" aria-label="本机服务管理">
      <RuntimeSetup disabled={working} preparing={management?.busy === true && management.phase === "preparing"} configured={management?.configured === true} onPrepare={(write, execute) => operate("prepare", write, execute)}/>
      <div className="runtime-management-heading"><h2>本机服务</h2><span>{management?.busy ? "操作中" : management?.observed ? management.running ? "全部就绪" : "尚未全部就绪" : "状态未确认"}</span></div>
      <p className="runtime-directory">{management?.directory || "选择已配置的 ProofCode 部署目录，即可在登录前启动服务。"}</p>
      <div className="runtime-management-actions">
        <button className="secondary-button" disabled={working} onClick={() => void operate("select")}><FolderOpen size={14}/>{management?.configured ? "更换部署目录" : "选择部署目录"}</button>
        <button className="primary-button" disabled={working || !management?.configured} onClick={() => void operate("start")}><Play size={14}/>启动服务</button>
        <button className="secondary-button" disabled={working || !management?.configured} onClick={() => void operate("stop")}><Square size={14}/>停止服务</button>
      </div>
      {management?.busy && <p className="runtime-environment-note" role="status"><Loader2 size={15}/>{phases[management.phase] || "服务操作正在进行…"}</p>}
      {(managementError || management?.message) && <p className="runtime-environment-note" role={managementError || management?.phase === "error" ? "alert" : "status"}>{management?.message || managementError}</p>}
      {!!management?.services?.length && <div className="runtime-service-list" aria-label="最近读取的容器状态">{management.services.map(service => <div key={service.name}><strong>{service.name}</strong><span>{serviceStates[service.state] || "未知"}{service.health ? ` / ${healthStates[service.health] || "健康状态未知"}` : ""}</span></div>)}</div>}
      {management?.checkedAt && <p className="runtime-environment-note">{management.observed && !management.busy ? "状态读取时间" : "上次状态读取时间"}：{new Date(management.checkedAt).toLocaleString()}</p>}
      <p className="runtime-environment-note">启动会构建并运行所选目录的服务；停止会保留数据库和文件。内置运行包已生成本机配置；自行选择的部署目录需先配置 .env。启动服务前，请先启动 Docker Desktop。</p>
    </section>}
    {report && <>
      <div className="runtime-environment-status"><span><Wrench size={15}/> {report.checks.filter(check => check.state === "ready").length} / {report.checks.length} 项可用</span><small>{busy || error ? "上次检查时间" : "检查时间"}：{new Date(report.checkedAt).toLocaleString()}</small></div>
      <div className="runtime-check-list">{report.checks.map(check => <article className={`runtime-check is-${check.state}`} key={check.id}><span className="runtime-check-icon">{check.state === "ready" ? <Check size={15}/> : <AlertCircle size={15}/>}</span><div><strong>{check.name}</strong><small>{check.detail}</small></div><span className="runtime-check-state">{states[check.state] || "未知"}</span></article>)}</div>
      <p className="runtime-environment-note">环境检查不会自动启动或停止服务。模型服务与 API Key 在“Agent 设置”中单独配置。</p>
    </>}
  </section>;
}
