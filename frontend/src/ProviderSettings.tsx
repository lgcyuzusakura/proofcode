import { useEffect, useState } from "react";
import { Check, ChevronDown, Loader2, Plus, RefreshCw, Trash2, XCircle } from "lucide-react";
import { api, desktopApp, post, token } from "./api";
import type { ModelConfig, ModelProvider, ModelSettings, PriceTier } from "./types";

const defaultSettings = (): ModelSettings => ({ contextTokens: 128000, maxInputTokens: 16000, maxOutputTokens: 2048, compression: "balanced", currency: "CNY", prices: [] });
const cloneSettings = (value: ModelSettings | undefined): ModelSettings => ({ ...defaultSettings(), ...(value || {}), prices: (value?.prices || []).map(row => ({ ...row })) });
const prettyTokens = (value: number) => value >= 1000000 ? `${(value / 1000000).toFixed(value % 1000000 ? 1 : 0)}m` : value >= 1000 ? `${Math.round(value / 1000)}k` : String(value);

export function ProviderSettings() {
  const [providers, setProviders] = useState<ModelProvider[]>([]);
  const [selected, setSelected] = useState<ModelProvider | null>(null);
  const [name, setName] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [wireApi, setWireApi] = useState<ModelProvider["wireApi"]>("chat");
  const [aliases, setAliases] = useState<Record<string, string>>({});
  const [modelSettings, setModelSettings] = useState<Record<string, ModelSettings>>({});
  const [settingsModel, setSettingsModel] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [localConfig, setLocalConfig] = useState<ModelConfig | null>(null);
  const dirty = !!selected && (name !== selected.name || baseUrl !== selected.baseUrl || wireApi !== selected.wireApi || !!apiKey || JSON.stringify(aliases) !== JSON.stringify(selected.aliases) || JSON.stringify(modelSettings) !== JSON.stringify(selected.modelSettings || {}));
  const edit = (p: ModelProvider | null) => { const settings = p?.modelSettings || {}; setSelected(p); setName(p?.name || ""); setBaseUrl(p?.baseUrl || ""); setWireApi(p?.wireApi || "chat"); setAliases(p?.aliases || {}); setModelSettings(settings); setSettingsModel(p?.models[0]?.id || Object.keys(settings)[0] || ""); setApiKey(""); setError(""); setNotice(""); };
  useEffect(() => {
    let live = true;
    void api<ModelProvider[]>("/api/model-providers").then(values => { if (live) { setProviders(values); edit(values[0] || null); } }).catch(e => { if (live) setError(e instanceof Error ? e.message : String(e)); });
    void desktopApp()?.GetModelConfig?.().then(value => { if (live) setLocalConfig(value); }).catch(() => undefined);
    return () => { live = false; };
  }, []);
  const update = (value: ModelProvider) => { setProviders(old => old.some(p => p.id === value.id) ? old.map(p => p.id === value.id ? value : p) : [...old, value]); window.dispatchEvent(new Event("proofcode:providers-changed")); };
  const save = async () => {
    setBusy(true); setError(""); setNotice("");
    try {
      const value = await api<ModelProvider>(selected ? `/api/model-providers/${selected.id}` : "/api/model-providers", { method: selected ? "PATCH" : "POST", body: JSON.stringify({ name, baseUrl, apiKey, wireApi, aliases, modelSettings }) });
      update(value); edit(value); setNotice("服务已保存。点击「读取可用模型」后即可在对话中选择。");
    } catch (e) { setError(e instanceof Error ? e.message : String(e)); } finally { setBusy(false); }
  };
  const activeSettings = settingsModel ? cloneSettings(modelSettings[settingsModel]) : null;
  const updateSettings = (change: Partial<ModelSettings>) => { if (!settingsModel || !activeSettings) return; setModelSettings(old => ({ ...old, [settingsModel]: { ...activeSettings, ...change } })); };
  const updatePrice = (index: number, change: Partial<PriceTier>) => { if (!activeSettings) return; const prices = activeSettings.prices.map((row, rowIndex) => rowIndex === index ? { ...row, ...change } : row); updateSettings({ prices }); };
  const addPrice = () => { if (!activeSettings || activeSettings.prices.length >= 32) return; updateSettings({ prices: [...activeSettings.prices, { upToInputTokens: 0, input: 0, output: 0, cacheRead: null, cacheWrite: null }] }); };
  const removePrice = (index: number) => { if (!activeSettings) return; updateSettings({ prices: activeSettings.prices.filter((_, rowIndex) => rowIndex !== index) }); };
  const refresh = async () => {
    if (!selected || dirty) return;
    setBusy(true); setError(""); setNotice("");
    try { const value = await post<ModelProvider>(`/api/model-providers/${selected.id}/models/refresh`, {}); update(value); setSelected(value); setSettingsModel(old => old || value.models[0]?.id || ""); setNotice(`已读取 ${value.models.length} 个可用模型。`); } catch (e) { setError(e instanceof Error ? e.message : String(e)); } finally { setBusy(false); }
  };
  const remove = async () => {
    if (!selected || !window.confirm(`删除模型服务「${selected.name}」？`)) return;
    setBusy(true); setError("");
    try { await post(`/api/model-providers/${selected.id}/delete`, {}); setProviders(old => old.filter(p => p.id !== selected.id)); edit(null); window.dispatchEvent(new Event("proofcode:providers-changed")); } catch (e) { setError(e instanceof Error ? e.message : String(e)); } finally { setBusy(false); }
  };
  const importLocal = async () => {
    const bridge = desktopApp()?.ImportCodexProvider;
    if (!bridge) return;
    setBusy(true); setError("");
    try { const value = await bridge(token()); update(value); edit(value); setNotice("本机 Codex 配置已导入，密钥由桌面端直接发送到控制面。请读取可用模型。"); } catch (e) { setError(e instanceof Error ? e.message : String(e)); } finally { setBusy(false); }
  };
  const probe = async () => {
    if (!selected || dirty || !settingsModel) return;
    setBusy(true); setError(""); setNotice("");
    try {
      const result = await post<{ success: boolean; outcome: string; durationMs: number; httpStatus: number }>(`/api/model-providers/${selected.id}/probe`, { model: settingsModel });
      const reasons: Record<string, string> = { authentication: "密钥认证失败", permission_or_policy: "服务拒绝访问或策略拦截", rate_or_overload: "服务限流或繁忙", request_or_context: "服务不接受测试请求", endpoint_or_model: "接口或模型不存在", provider: "服务返回错误", tool_protocol: "返回内容未通过工具调用格式检查", timeout: "请求超时", interrupted: "请求中断", connection: "服务连接失败", response_limit: "服务响应超过测试大小限制" };
      if (result.success) setNotice(`${settingsModel} 的最小工具调用测试通过，用时 ${result.durationMs} ms。未执行工具。`);
      else setError(`测试未通过：${reasons[result.outcome] || "服务响应无效"}${result.httpStatus ? `（HTTP ${result.httpStatus}）` : ""}。`);
    } catch (e) { setError(e instanceof Error ? e.message : String(e)); } finally { setBusy(false); }
  };
  return <div className="provider-settings">
    <div className="provider-list">
      {providers.map(p => <button key={p.id} disabled={busy} className={selected?.id === p.id ? "active" : ""} onClick={() => edit(p)}><strong>{p.name}</strong><small>{p.models.length} 个模型 · {p.wireApi}</small></button>)}
      <button className="secondary-button" disabled={busy} onClick={() => edit(null)}><Plus size={14}/>添加模型服务</button>
      {localConfig && <div className="local-model-config"><small>本机 Codex · {localConfig.provider || "未配置"}</small><p>{localConfig.baseUrl}</p><button className="secondary-button" disabled={busy || !localConfig.configured} onClick={() => void importLocal()}>导入本机配置</button></div>}
    </div>
    <div className="provider-form">
      <label>服务名称<input disabled={busy} maxLength={120} value={name} onChange={e => setName(e.target.value)} placeholder="例如 DeepSeek / Kimi / 自定义服务"/></label>
      <label>API Base URL<input disabled={busy} value={baseUrl} onChange={e => setBaseUrl(e.target.value)} placeholder="https://api.example.com/v1"/></label>
      <label>API Key<input disabled={busy} type="password" autoComplete="new-password" value={apiKey} onChange={e => setApiKey(e.target.value)} placeholder={selected?.hasKey ? "已保存，留空表示保留" : "输入服务的 API Key"}/></label>
      <label>协议<select disabled={busy} value={wireApi} onChange={e => setWireApi(e.target.value as ModelProvider["wireApi"])}><option value="responses">Responses</option><option value="chat">Chat Completions</option><option value="anthropic">Anthropic Messages</option></select></label>
      {wireApi === "anthropic" && <p className="form-help">填写以 /v1 结尾的服务地址。使用 Messages 协议与 API Key 认证；模型从该服务的目录读取。</p>}
      <p className="form-help">保存后读取服务的可用模型。API Key 加密存储在控制面，页面不会回显。</p>
      <div className="panel-actions"><button className="primary-button" disabled={busy || !name.trim() || !baseUrl.trim() || (!selected && !apiKey)} onClick={() => void save()}>{busy ? <Loader2 size={14}/> : <Check size={14}/>}保存服务</button>{selected && <><button className="secondary-button" disabled={busy || dirty} onClick={() => void refresh()}><RefreshCw size={14}/>读取可用模型</button><button className="icon-button" aria-label="删除模型服务" title="删除模型服务" disabled={busy} onClick={() => void remove()}><Trash2 size={14}/></button></>}</div>
      {dirty && <p className="form-help">配置已修改，请先保存，再读取可用模型。</p>}
      {selected && selected.models.length > 0 && <div className="provider-probe"><label>测试模型<select disabled={busy} value={settingsModel} onChange={e => setSettingsModel(e.target.value)}>{selected.models.map(model => <option key={model.id} value={model.id}>{aliases[model.id] || model.name}</option>)}</select></label><button type="button" className="secondary-button" disabled={busy || dirty || !settingsModel} onClick={() => void probe()}>验证工具调用</button><p className="form-help">发送少量测试文本，可能产生模型费用；不发送项目内容，不执行工具。结果只验证这一次调用。</p></div>}
      {notice && <div className="capsule-success"><Check size={15}/>{notice}</div>}
      {error && <div className="capsule-error"><XCircle size={15}/>{error}</div>}
      {selected && <div className="provider-models"><div className="settings-section-head"><div><strong>模型运行参数</strong><span>每个模型单独保存上下文、输出上限、压缩与计价</span></div></div>{selected.models.length === 0 && <p className="form-help">先读取可用模型，才能为模型添加配置。</p>}{selected.models.map(m => <div className="provider-model-row" key={m.id}><label><code>{m.id}</code><input disabled={busy} maxLength={120} aria-label={`${m.id} 显示名称`} value={aliases[m.id] || ""} onChange={e => setAliases(old => ({ ...old, [m.id]: e.target.value }))} placeholder="模型显示名称"/></label><button type="button" className={`model-settings-trigger ${settingsModel === m.id ? "active" : ""}`} disabled={busy} onClick={() => { setSettingsModel(m.id); if (!modelSettings[m.id]) setModelSettings(old => ({ ...old, [m.id]: defaultSettings() })); }}><span>{modelSettings[m.id] ? `${prettyTokens(modelSettings[m.id].contextTokens ?? 128000)} · ${{gentle:"轻度",balanced:"均衡",aggressive:"强力"}[modelSettings[m.id].compression]}` : "设置参数"}</span><ChevronDown size={14}/></button></div>)}</div>}
      {activeSettings && <fieldset disabled={busy} className="model-settings-editor"><div className="settings-editor-title"><div><strong>{settingsModel}</strong><span>可选配置：留空沿用默认预算</span></div><span className="settings-badge">{activeSettings.currency}</span></div><div className="settings-grid"><label>上下文最大 Token<input type="number" min="1024" max="2000000" placeholder="默认 128000" value={activeSettings.contextTokens ?? ""} onChange={e => updateSettings({ contextTokens: e.target.value === "" ? null : Number(e.target.value) })}/></label><label>输入最大 Token<input type="number" min="256" placeholder="默认 16000" value={activeSettings.maxInputTokens ?? ""} onChange={e => updateSettings({ maxInputTokens: e.target.value === "" ? null : Number(e.target.value) })}/></label><label>输出最大 Token<input type="number" min="1" placeholder="默认 2048" value={activeSettings.maxOutputTokens ?? ""} onChange={e => updateSettings({ maxOutputTokens: e.target.value === "" ? null : Number(e.target.value) })}/></label><label>计价币种<input maxLength={3} value={activeSettings.currency} onChange={e => updateSettings({ currency: e.target.value.toUpperCase() })}/></label><label>上下文压缩<select aria-label="上下文压缩" value={activeSettings.compression} onChange={e => updateSettings({ compression: e.target.value as ModelSettings["compression"] })}><option value="gentle">轻度 · 保留 100%</option><option value="balanced">均衡 · 保留 80%</option><option value="aggressive">强力 · 保留 60%</option></select></label></div><p className="form-help">默认上下文 128k、输入 16k、输出 2048 Token。输入 + 输出 + 256 Token 不得超过上下文上限。压缩比例以输入预算为基准，必须保留的内容可使用完整输入预算。</p><div className="price-editor"><div className="price-editor-head"><div><strong>输入价格区间</strong><span>单位：每 1M Token；按单次输入量整档计价，上限递增，最后填 0 表示不限</span></div><button type="button" className="secondary-button" onClick={addPrice}><Plus size={13}/>增加区间</button></div>{activeSettings.prices.map((row, index) => <div className="price-row" key={index}><label>输入上限<input type="number" min="0" value={row.upToInputTokens} onChange={e => updatePrice(index, { upToInputTokens: Number(e.target.value) })}/></label><label>输入<input type="number" min="0" step="any" value={row.input} onChange={e => updatePrice(index, { input: Number(e.target.value) })}/></label><label>输出<input type="number" min="0" step="any" value={row.output} onChange={e => updatePrice(index, { output: Number(e.target.value) })}/></label><label>缓存读<input type="number" min="0" step="any" placeholder="同输入" value={row.cacheRead ?? ""} onChange={e => updatePrice(index, { cacheRead: e.target.value === "" ? null : Number(e.target.value) })}/></label><label>缓存写<input type="number" min="0" step="any" placeholder="同输入" value={row.cacheWrite ?? ""} onChange={e => updatePrice(index, { cacheWrite: e.target.value === "" ? null : Number(e.target.value) })}/></label><button type="button" className="icon-button" aria-label="删除价格区间" title="删除价格区间" onClick={() => removePrice(index)}><Trash2 size={14}/></button></div>)}{!activeSettings.prices.length && <p className="form-help">暂未设置价格，费用会显示为未估算。</p>}</div></fieldset>}
    </div>
  </div>;
}
