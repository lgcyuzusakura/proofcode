import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createHash, webcrypto } from "node:crypto";
import test from "node:test";
import ts from "typescript";

globalThis.crypto ??= webcrypto;
const storage = new Map();
globalThis.localStorage = { getItem: key => storage.get(key) ?? null, setItem: (key, value) => storage.set(key, value), removeItem: key => storage.delete(key) };
const source = await readFile(new URL("../src/source-attachment-model.ts", import.meta.url), "utf8");
const javascript = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText;
const model = await import(`data:text/javascript;base64,${Buffer.from(javascript).toString("base64")}`);
const attachment = (index = 0) => ({ sourceSnapshotId: "snapshot", path: `src/file-${index}.ts`, sha256: "a".repeat(64), startLine: 1, endLine: 2 });

test("attachment drafts preserve exact scope, deduplicate, cap at eight and remove explicitly", () => {
  storage.clear();
  for (let index = 0; index < 8; index++) model.addSourceAttachment("project", "chat", attachment(index));
  model.addSourceAttachment("project", "chat", attachment());
  assert.equal(model.readSourceAttachments("project", "chat").items.length, 8);
  assert.throws(() => model.addSourceAttachment("project", "chat", attachment(8)), /8/);
  assert.deepEqual(model.readSourceAttachments("other", "chat").items, []);
  assert.deepEqual(model.readSourceAttachments("project", "other").items, []);
  model.saveSourceAttachments("project", "chat", []);
  assert.deepEqual(model.readSourceAttachments("project", "chat").items, []);
});

test("corrupt drafts and invalid ranges report errors and can be cleared", () => {
  storage.clear();storage.set("proofcode.chat-attachments.project.chat", "{bad");
  assert.ok(model.readSourceAttachments("project", "chat").error);
  assert.throws(() => model.addSourceAttachment("project", "chat", attachment()), /草稿/);
  model.saveSourceAttachments("project", "chat", []);
  assert.equal(model.readSourceAttachments("project", "chat").error, undefined);
  assert.throws(() => model.saveSourceAttachments("project", "chat", [{...attachment(), startLine: 3, endLine: 2}]), /无效/);
  assert.throws(() => model.saveSourceAttachments("project", "chat", [{...attachment(), sha256: "incorrect"}]), /无效/);
});

test("source decoding verifies original bytes before normalizing CRLF for selection", async () => {
  const bytes = Buffer.from("一\r\n二\r三\n");
  const file = {sha256: createHash("sha256").update(bytes).digest("hex"), content: bytes.toString("base64")};
  assert.equal(model.sourceExcerpt(await model.verifiedSourceText(file), {startLine: 2, endLine: 3}), "二\n三");
  await assert.rejects(model.verifiedSourceText({...file, content: Buffer.from("tampered").toString("base64")}), /校验/);
  const invalid = Buffer.from([0xff, 0xfe]);
  await assert.rejects(model.verifiedSourceText({sha256: createHash("sha256").update(invalid).digest("hex"), content: invalid.toString("base64")}));
  assert.throws(() => model.sourceExcerpt("one\ntwo", {startLine: 3}), /选区/);
  assert.throws(() => model.sourceExcerpt("x".repeat(32769), {}), /32 KiB/);
  assert.throws(() => model.sourceExcerpt("line\n".repeat(301), {}), /300/);
});
