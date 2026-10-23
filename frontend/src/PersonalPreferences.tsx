import { useEffect, useState } from "react";
import { Save } from "lucide-react";
import { api } from "./api";

export type PreferenceValues = { responseLanguage: string; answerStyle: string; codeStyle: string; collaborationStyle: string };
export type Personalization = { version: number; values: PreferenceValues };
const emptyValues = (): PreferenceValues => ({ responseLanguage: "", answerStyle: "", codeStyle: "", collaborationStyle: "" });
const preferenceFields: { key: keyof PreferenceValues; label: string }[] = [
 { key: "responseLanguage", label: "回答语言" }, { key: "answerStyle", label: "回答方式" }, { key: "codeStyle", label: "编码偏好" }, { key: "collaborationStyle", label: "协作方式" }
];

export function PreferenceEvidence({ value }: { value?: Personalization | null }) {
 if (!value?.values || !preferenceFields.some(field => value.values[field.key])) return null;
 return <article className="personal-preference-evidence"><strong>个人偏好</strong><small>当前账户 · 版本 {value.version} · 由你明确保存</small><dl>{preferenceFields.filter(field => value.values[field.key]).map(field => <div key={field.key}><dt>{field.label}</dt><dd>{value.values[field.key]}</dd></div>)}</dl></article>;
}

export function PersonalPreferences({ onBusy }: { onBusy: (value: boolean) => void }) {
 const [saved, setSaved] = useState<Personalization>({ version: 0, values: emptyValues() }), [values, setValues] = useState<PreferenceValues>(emptyValues);
 const [loading, setLoading] = useState(true), [loaded, setLoaded] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState(""), [notice, setNotice] = useState("");
 const load = async () => { setLoading(true); setError(""); setNotice(""); try { const result = await api<Personalization>("/api/account/preferences"); if (!result?.values) throw new Error("个人偏好响应无效，请重试。"); setSaved(result); setValues(result.values); setLoaded(true); } catch (cause) { setError(String(cause)); } finally { setLoading(false); } };
 useEffect(() => { let live = true; void api<Personalization>("/api/account/preferences").then(result => { if (!result?.values) throw new Error("个人偏好响应无效，请重试。"); if (live) { setSaved(result); setValues(result.values); setLoaded(true); } }).catch(cause => { if (live) setError(String(cause)); }).finally(() => { if (live) setLoading(false); }); return () => { live = false; }; }, []);
 const save = async () => { if (!loaded || loading || busy) return; setBusy(true); onBusy(true); setError(""); setNotice(""); try { const result = await api<Personalization>("/api/account/preferences", { method: "PUT", body: JSON.stringify({ version: saved.version, values }) }); setSaved(result); setValues(result.values); setNotice("个人偏好已保存，新任务会使用这份设置。"); } catch (cause) { setError(String(cause)); } finally { setBusy(false); onBusy(false); } };
 return <div className="agent-platform-body personal-preferences-body"><aside><strong>跟随你的工作方式</strong><p>用于当前账户的所有项目。项目约束和当前消息优先。</p><p>这些内容由你填写；聊天不会自动写入。</p></aside><form onSubmit={event => { event.preventDefault(); void save(); }}><fieldset disabled={busy || loading || !loaded}>
  <label>回答语言<input aria-label="回答语言" maxLength={80} value={values.responseLanguage} onChange={event => setValues({ ...values, responseLanguage: event.target.value })} placeholder="例如：中文，代码标识符保持英文"/></label>
  <label>回答方式<textarea aria-label="回答方式" rows={3} maxLength={2000} value={values.answerStyle} onChange={event => setValues({ ...values, answerStyle: event.target.value })} placeholder="例如：先说结果，再给关键依据；避免重复解释"/></label>
  <label>编码偏好<textarea aria-label="编码偏好" rows={3} maxLength={2000} value={values.codeStyle} onChange={event => setValues({ ...values, codeStyle: event.target.value })} placeholder="例如：优先沿用项目规范，少引入依赖"/></label>
  <label>协作方式<textarea aria-label="协作方式" rows={3} maxLength={2000} value={values.collaborationStyle} onChange={event => setValues({ ...values, collaborationStyle: event.target.value })} placeholder="例如：完成后说明修改范围、验证结果和剩余问题"/></label>
  <footer><button type="submit" className="primary-button" disabled={!loaded || loading || busy}><Save size={14}/>{busy ? "保存中…" : "保存个人偏好"}</button><button type="button" disabled={!loaded || loading || busy} onClick={() => { setValues(emptyValues()); setNotice("内容已清空，保存后新任务将不再使用个人偏好。"); }}>清空内容</button></footer>
 </fieldset>{loading && <p>正在读取个人偏好…</p>}{error && <p role="alert" className="agent-platform-error">{error}</p>}{error && <button type="button" disabled={busy || loading} onClick={() => void load()}>重新加载个人偏好</button>}{notice && <p role="status">{notice}</p>}<p className="agent-platform-note">全部留空即可关闭。新任务保存偏好快照，正在执行和重试的任务保持原设置。偏好不能改变工具权限与审批规则。</p></form></div>;
}
