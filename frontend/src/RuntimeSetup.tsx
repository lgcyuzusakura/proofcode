import { useEffect, useRef, useState } from "react";
import { Download, ExternalLink, Loader2, PackageOpen } from "lucide-react";
import { desktopApp } from "./api";
import type { BundledRuntime } from "./types";

const dependencies = [
  { name: "Docker Desktop", href: "https://docs.docker.com/desktop/setup/install/windows-install/", text: "本机服务需要 Docker Desktop，安装后启动它。首次构建会下载镜像与依赖。" },
  { name: "Git for Windows", href: "https://git-scm.com/downloads/win", text: "本机代码修改、补丁应用和 Git 操作需要 Git。" },
];
export function RuntimeSetup({ disabled, preparing, configured, onPrepare }: { disabled: boolean; preparing: boolean; configured: boolean; onPrepare: (write: boolean, execute: boolean) => Promise<void> }) {
  const [bundle, setBundle] = useState<BundledRuntime | null>(null), [error, setError] = useState("");
  const [write, setWrite] = useState(true), [execute, setExecute] = useState(false), [loaded, setLoaded] = useState(false);
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    const get = desktopApp()?.GetBundledRuntime;
    if (!get) { setLoaded(true); return () => { mounted.current = false; }; }
    void get().then(value => { if (mounted.current) setBundle(value); }).catch(() => { if (mounted.current) setError("内置运行包无法读取，请使用完整发行包，或选择已有部署目录。"); }).finally(() => { if (mounted.current) setLoaded(true); });
    return () => { mounted.current = false; };
  }, []);
  return <details className="runtime-setup" open={!configured}>
    <summary><PackageOpen size={16}/>首次使用与依赖准备</summary>
    <div className="runtime-setup-body">
      <p>本机部署需要 Docker Desktop 和 Git，无需自行安装 Node、Go 或 Java。准备完成后注册账户，在模型设置中填写 API Key。</p>
      <div className="runtime-dependency-links">{dependencies.map(item => <div key={item.name}><div><strong>{item.name}</strong><p>{item.text}</p></div><a href={item.href} target="_blank" rel="noopener noreferrer" title={`打开 ${item.name} 安装说明`}><ExternalLink size={14}/>安装说明</a></div>)}</div>
      {!loaded ? <p role="status">正在读取内置运行包…</p> : error ? <p role="alert">{error}</p> : bundle?.available ? <>
        <div className="runtime-bundle-label"><Download size={15}/><strong>内置服务 {bundle.release}</strong><span>{(bundle.bytes / 1024 / 1024).toFixed(1)} MB</span></div>
        <p>选择保存位置后创建新的 ProofCode-runtime 文件夹，自动生成本机密钥。已有目录不会覆盖，也不会自动启动服务。</p>
        <fieldset disabled={disabled} className="runtime-setup-permissions"><legend>代码任务能力</legend><label><input type="checkbox" checked={write} onChange={event => setWrite(event.target.checked)}/>允许生成并应用代码补丁</label><label><input type="checkbox" checked={execute} onChange={event => setExecute(event.target.checked)}/>允许执行命令和测试</label><small>实际调用仍遵循模式、工具白名单和审批规则；生成的配置可在 .env 中调整。</small></fieldset>
        <button className="secondary-button" disabled={disabled || !desktopApp()?.PrepareBundledRuntime} onClick={() => void onPrepare(write, execute)}>{preparing ? <Loader2 size={15} className="spinning"/> : <PackageOpen size={15}/>}准备本机运行文件</button>
      </> : <p className="runtime-environment-note">此开发版本没有内置服务文件，可选择已有 ProofCode 部署目录。正式构建会包含运行包。</p>}
    </div>
  </details>;
}
