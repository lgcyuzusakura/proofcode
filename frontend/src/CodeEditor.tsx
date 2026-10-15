import { useContext, useEffect, useRef } from "react";
import { desktopApp } from "./api";
import { ListFilter } from "lucide-react";
import { IDEProtocolContext, protocolRequest } from "./ProtocolTools";
import * as monaco from "monaco-editor/esm/vs/editor/editor.api";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import TSWorker from "monaco-editor/esm/vs/language/typescript/ts.worker?worker";
import JSONWorker from "monaco-editor/esm/vs/language/json/json.worker?worker";
import "monaco-editor/esm/vs/basic-languages/monaco.contribution";
import "monaco-editor/esm/vs/language/typescript/monaco.contribution";
import "monaco-editor/esm/vs/language/json/monaco.contribution";
// editor.api is intentionally tree-shaken; these contributions register the
// built-in suggest and hover controllers used by the LSP providers below.
import "monaco-editor/esm/vs/editor/contrib/suggest/browser/suggestController";
import "monaco-editor/esm/vs/editor/contrib/hover/browser/hoverContribution";
import "monaco-editor/min/vs/editor/editor.main.css";
const workerScope = self as unknown as { MonacoEnvironment: { getWorker: (_: string, label: string) => Worker } };
workerScope.MonacoEnvironment = { getWorker: (_, label) => label === "typescript" || label === "javascript" ? new TSWorker() : label === "json" ? new JSONWorker() : new EditorWorker() };
function language(path: string) { const extension = path.split(".").pop() || ""; return ({ts:"typescript",tsx:"typescript",js:"javascript",jsx:"javascript",json:"json",py:"python",go:"go",java:"java",rs:"rust",md:"markdown",yml:"yaml",yaml:"yaml",css:"css",html:"html",sql:"sql",ps1:"powershell",sh:"shell",toml:"ini",cs:"csharp"} as Record<string,string>)[extension] || "plaintext"; }
export default function CodeEditor({ fileId, path, initial, readOnly, onChange, onSave }: { fileId: string; path: string; initial: string; readOnly: boolean; onChange: (value: string) => void; onSave: () => void }) {
  const container = useRef<HTMLDivElement>(null); const callbacks = useRef({ onChange, onSave }); callbacks.current = { onChange, onSave };
  const editor = useRef<monaco.editor.IStandaloneCodeEditor | undefined>(undefined);
  const protocol=useContext(IDEProtocolContext);const protocolRef=useRef(protocol);protocolRef.current=protocol;
  useEffect(() => {
    if (!container.current) return;
    const model = monaco.editor.createModel(initial, language(path));
    const instance = monaco.editor.create(container.current, { model, automaticLayout: true, readOnly, glyphMargin:true, fontSize: 13, minimap: { enabled: false }, scrollBeyondLastLine: false, tabSize: 2, wordWrap: "off", padding: {top:16}, renderWhitespace: "selection" });
    editor.current = instance;
    const change = instance.onDidChangeModelContent(() => callbacks.current.onChange(instance.getValue()));
    instance.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => callbacks.current.onSave());
    const click=instance.onMouseDown(event=>{if(event.target.type===monaco.editor.MouseTargetType.GUTTER_GLYPH_MARGIN&&event.target.position)protocolRef.current.toggleBreakpoint(path,event.target.position.lineNumber);});
    return () => { click.dispose();change.dispose(); instance.dispose(); model.dispose(); editor.current = undefined; };
  }, [fileId]);
  useEffect(() => { editor.current?.updateOptions({ readOnly }); }, [readOnly]);
  useEffect(()=>{if(protocol.line&&editor.current){editor.current.setPosition({lineNumber:protocol.line,column:1});editor.current.revealLineInCenter(protocol.line);}},[protocol.line,fileId]);
  useEffect(()=>{const instance=editor.current;if(!instance)return;const decorations=instance.createDecorationsCollection((protocol.breakpoints[path]||[]).map(line=>({range:new monaco.Range(line,1,line,1),options:{glyphMarginClassName:"debug-breakpoint",glyphMarginHoverMessage:{value:"断点"}}})));return()=>decorations.clear();},[protocol.breakpoints,path,fileId]);
  useEffect(()=>{
    const instance=editor.current,session=protocol.session,model=instance?.getModel();if(!instance||!model||!session||language(path)!==session.language)return;
    const uri=`${session.rootUri.replace(/\/$/,"")}/${path.split("/").map(encodeURIComponent).join("/")}`;let version=1,closed=false,after=0,changing=false,changed=false,lastText=model.getValue();let timer:ReturnType<typeof setTimeout>|undefined;
    const document={uri,languageId:session.language,version,text:model.getValue()};
    const notify=(method:string,params:unknown)=>protocolRequest(session.handle,session.id,method,params,true);
    const opened=session.openClose?notify("textDocument/didOpen",{textDocument:document}):Promise.resolve();void opened.catch(()=>{});
    const flush=async()=>{if(timer){clearTimeout(timer);timer=undefined;}await opened;if(closed||!changed||session.syncKind===0)return;const text=model.getValue();changed=false;const end=lastText.split("\n"),range={start:{line:0,character:0},end:{line:end.length-1,character:end[end.length-1].replace(/\r$/,"").length}};lastText=text;await notify("textDocument/didChange",{textDocument:{uri,version:++version},contentChanges:[session.syncKind===2?{range,text}:{text}]});};
    const change=model.onDidChangeContent(()=>{changed=true;if(timer)clearTimeout(timer);timer=setTimeout(()=>void flush().catch(()=>{}),250);});
    const request=async(method:string,position:monaco.Position)=>{await flush();return protocolRequest(session.handle,session.id,method,{textDocument:{uri},position:{line:position.lineNumber-1,character:position.column-1}});};
    const completion=monaco.languages.registerCompletionItemProvider(session.language,{triggerCharacters:[".",":"],provideCompletionItems:async(current,position)=>{if(current!==model||closed)return {suggestions:[]};try{const result=await request("textDocument/completion",position);const word=model.getWordUntilPosition(position);return {suggestions:(Array.isArray(result)?result:result?.items||[]).map((item:any)=>({label:item.label,kind:monaco.languages.CompletionItemKind.Text,detail:item.detail,documentation:typeof item.documentation==="string"?item.documentation:item.documentation?.value,insertText:item.textEdit?.newText||item.insertText||item.label,range:item.textEdit?.range?new monaco.Range(item.textEdit.range.start.line+1,item.textEdit.range.start.character+1,item.textEdit.range.end.line+1,item.textEdit.range.end.character+1):new monaco.Range(position.lineNumber,word.startColumn,position.lineNumber,word.endColumn)}))};}catch{return {suggestions:[]};}}});
    const hover=monaco.languages.registerHoverProvider(session.language,{provideHover:async(current,position)=>{if(current!==model||closed)return null;try{const result=await request("textDocument/hover",position);const values=Array.isArray(result?.contents)?result.contents:[result?.contents];return {contents:values.filter(Boolean).map((value:any)=>({value:typeof value==="string"?value:value.value}))};}catch{return null;}}});
    const f12=instance.addAction({id:"proofcode.lsp.definition",label:"LSP: 跳转到定义",keybindings:[monaco.KeyCode.F12],run:async()=>{const position=instance.getPosition();if(!position)return;try{const result=await request("textDocument/definition",position);const value=Array.isArray(result)?result[0]:result;const target=value?.uri||value?.targetUri;if(!target)return;const root=session.rootUri.replace(/\/$/,"")+"/";if(!target.startsWith(root))return;const relative=decodeURIComponent(target.slice(root.length)),line=(value.range||value.targetSelectionRange)?.start.line+1;if(relative===path)instance.revealLineInCenter(line||1);else protocolRef.current.onNavigate(relative,line);}catch{/* Server errors leave the editor intact. */}}});
    const poll=setInterval(()=>{if(changing||closed)return;changing=true;void desktopApp()!.GetProjectProtocol!(session.handle,session.id,after).then(state=>{if(closed)return;after=state.sequence;for(const event of state.events){if(event.message.method!=="textDocument/publishDiagnostics"||event.message.params?.uri!==uri)continue;const diagnostics=event.message.params.diagnostics||[];monaco.editor.setModelMarkers(model,"proofcode-lsp",diagnostics.map((value:any)=>({startLineNumber:value.range.start.line+1,startColumn:value.range.start.character+1,endLineNumber:value.range.end.line+1,endColumn:value.range.end.character+1,message:value.message,severity:value.severity===1?monaco.MarkerSeverity.Error:value.severity===2?monaco.MarkerSeverity.Warning:monaco.MarkerSeverity.Info,source:value.source,code:String(value.code||"")})));}}).catch(()=>{}).finally(()=>{changing=false;});},800);
    return()=>{closed=true;if(timer)clearTimeout(timer);clearInterval(poll);change.dispose();completion.dispose();hover.dispose();f12.dispose();monaco.editor.setModelMarkers(model,"proofcode-lsp",[]);if(session.openClose)void opened.then(()=>notify("textDocument/didClose",{textDocument:{uri}})).catch(()=>{});};
  },[protocol.session,fileId,path]);
  return <div className="monaco-shell"><div className="monaco-host" ref={container} aria-label={`代码编辑器 ${path}`}/><button type="button" className="icon-button editor-completion" title="代码补全" aria-label="代码补全" onClick={()=>{editor.current?.focus();editor.current?.trigger("proofcode","editor.action.triggerSuggest",{});}}><ListFilter size={14}/></button></div>;
}
