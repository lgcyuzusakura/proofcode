import { useEffect, useRef } from "react";
import * as monaco from "monaco-editor/esm/vs/editor/editor.api";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import TSWorker from "monaco-editor/esm/vs/language/typescript/ts.worker?worker";
import JSONWorker from "monaco-editor/esm/vs/language/json/json.worker?worker";
import "monaco-editor/esm/vs/basic-languages/monaco.contribution";
import "monaco-editor/esm/vs/language/typescript/monaco.contribution";
import "monaco-editor/esm/vs/language/json/monaco.contribution";
import "monaco-editor/min/vs/editor/editor.main.css";
const workerScope = self as unknown as { MonacoEnvironment: { getWorker: (_: string, label: string) => Worker } };
workerScope.MonacoEnvironment = { getWorker: (_, label) => label === "typescript" || label === "javascript" ? new TSWorker() : label === "json" ? new JSONWorker() : new EditorWorker() };
function language(path: string) { const extension = path.split(".").pop() || ""; return ({ts:"typescript",tsx:"typescript",js:"javascript",jsx:"javascript",json:"json",py:"python",go:"go",java:"java",rs:"rust",md:"markdown",yml:"yaml",yaml:"yaml",css:"css",html:"html",sql:"sql",ps1:"powershell",sh:"shell",toml:"ini",cs:"csharp"} as Record<string,string>)[extension] || "plaintext"; }
export default function CodeEditor({ fileId, path, initial, readOnly, onChange, onSave }: { fileId: string; path: string; initial: string; readOnly: boolean; onChange: (value: string) => void; onSave: () => void }) {
  const container = useRef<HTMLDivElement>(null); const callbacks = useRef({ onChange, onSave }); callbacks.current = { onChange, onSave };
  const editor = useRef<monaco.editor.IStandaloneCodeEditor | undefined>(undefined);
  useEffect(() => {
    if (!container.current) return;
    const model = monaco.editor.createModel(initial, language(path));
    const instance = monaco.editor.create(container.current, { model, automaticLayout: true, readOnly, fontSize: 13, minimap: { enabled: false }, scrollBeyondLastLine: false, tabSize: 2, wordWrap: "off", padding: {top:16}, renderWhitespace: "selection" });
    editor.current = instance;
    const change = instance.onDidChangeModelContent(() => callbacks.current.onChange(instance.getValue()));
    instance.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => callbacks.current.onSave());
    return () => { change.dispose(); instance.dispose(); model.dispose(); editor.current = undefined; };
  }, [fileId]);
  useEffect(() => { editor.current?.updateOptions({ readOnly }); }, [readOnly]);
  return <div className="monaco-host" ref={container} aria-label={`代码编辑器 ${path}`}/>;
}
