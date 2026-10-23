import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const source = await readFile(new URL("../src/editor-draft.ts", import.meta.url), "utf8");
const javascript = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText.replace('from "./types"', "");
const { parseEditorDraft, patchDraftBlock } = await import(`data:text/javascript;base64,${Buffer.from(javascript).toString("base64")}`);
const draft = (path, dirty = true) => JSON.stringify({ file: { path, content: "before\n", hash: "original" }, draft: dirty ? "edited locally\n" : "before\n" });

test("blocks a same-path dirty draft, including Windows path case and separators", () => {
  assert.match(patchDraftBlock(draft("src\\Main.ts"), ["src/main.ts"]), /未保存/);
  assert.equal(patchDraftBlock(draft("src/Main.ts"), ["src/other.ts"]), "");
});

test("saved and absent drafts do not block patch application", () => {
  assert.equal(patchDraftBlock(null, ["src/main.ts"]), "");
  assert.equal(patchDraftBlock(draft("src/main.ts", false), ["src/main.ts"]), "");
});

test("invalid draft state and ambiguous patch paths fail closed", () => {
  assert.equal(parseEditorDraft("{" ).kind, "invalid");
  assert.match(patchDraftBlock("{", ["src/main.ts"]), /无法确认/);
  assert.match(patchDraftBlock(draft("src/main.ts"), ["../main.ts"]), /无法确认/);
});
