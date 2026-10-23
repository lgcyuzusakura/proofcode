import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync } from "node:child_process";
import test from "node:test";
import ts from "typescript";

const source = await readFile(new URL("../src/patch-selection.ts", import.meta.url), "utf8");
const javascript = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText.replace('from "diff"', `from ${JSON.stringify(import.meta.resolve("diff"))}`);
const {reviewPatch, selectionIds, selectedPatch} = await import(`data:text/javascript;base64,${Buffer.from(javascript).toString("base64")}`);

async function fixture(files, run) {
  const directory = await mkdtemp(join(tmpdir(), "proofcode-review-"));
  const git = (...args) => execFileSync("git", ["-c", "core.hooksPath=", "-c", "core.autocrlf=false", ...args], {cwd: directory, encoding: "utf8", stdio: ["pipe", "pipe", "pipe"]});
  try {
    git("init", "--initial-branch=main");
    for (const [path, value] of Object.entries(files)) { await mkdir(join(directory, path, ".."), {recursive: true}); await writeFile(join(directory, path), value); }
    git("add", "-A"); git("-c", "user.name=ProofCode test", "-c", "user.email=test@local", "commit", "--allow-empty", "-m", "Temporary baseline");
    await run(directory, git, patch => execFileSync("git", ["-c", "core.autocrlf=false", "apply", "--index", "--binary", "-"], {cwd: directory, input: patch, encoding: "utf8", stdio: ["pipe", "pipe", "pipe"]}));
  } finally { await rm(directory, {recursive: true, force: true}); }
}

test("selected hunks apply through real git with insert/delete offsets and preserve rejected changes", async () => {
  const before = Array.from({length: 35}, (_, index) => `line-${index}\n`).join("");
  const after = before.replace("line-2\n", "line-2\ninserted\nextra\n").replace("line-16\n", "").replace("line-31", "replaced");
  await fixture({"src/main.txt": before}, async (directory, git, apply) => {
    await writeFile(join(directory, "src/main.txt"), after);const patch = git("diff", "--binary", "--no-ext-diff", "--no-color");git("checkout", "--", ".");
    const review = reviewPatch(patch);assert.equal(review.error, "");assert.equal(review.files[0].hunks.length, 3);
    const ids = selectionIds(review);assert.equal(selectedPatch(review, new Set(ids)), patch);assert.equal(selectedPatch(review, new Set()), "");
    for (const subset of [[ids[1]], [ids[2]], [ids[0], ids[2]], [ids[1], ids[2]]]) {
      const chosen = selectedPatch(review, new Set(subset));apply(chosen);
      const expected = before.replace("line-2\n", subset.includes(ids[0]) ? "line-2\ninserted\nextra\n" : "line-2\n").replace("line-16\n", subset.includes(ids[1]) ? "" : "line-16\n").replace("line-31", subset.includes(ids[2]) ? "replaced" : "line-31");
      assert.equal(await readFile(join(directory, "src/main.txt"), "utf8"), expected);git("reset", "--hard", "HEAD");
    }
    assert.throws(() => selectedPatch(review, new Set(["outdated"])), /变化/);
  });
});

test("multi-file patches retain raw binary, rename, create/delete and mode changes as whole files", async () => {
  await fixture({"old name.txt": "same\n", "deleted.txt": "remove\n", "binary.bin": Buffer.from([0,1,2,3]), "mode.sh": "echo hello\n"}, async (directory, git, apply) => {
    git("mv", "old name.txt", "new name.txt");await rm(join(directory,"deleted.txt"));await writeFile(join(directory,"new file.txt"),"new\n");await writeFile(join(directory,"binary.bin"),Buffer.from([0,7,8,9]));git("add","-A");git("update-index","--chmod=+x","mode.sh");
    const patch=git("diff","--cached","--binary","--no-ext-diff","--no-color");git("reset","--hard","HEAD");
    const review=reviewPatch(patch);assert.equal(review.error,"");assert.equal(review.files.length,5);assert(review.files.every(file=>file.atomicReason));
    assert.equal(selectedPatch(review,new Set(selectionIds(review))),patch);
    const wanted=review.files.filter(file=>["binary.bin","new name.txt","new file.txt"].includes(file.path));apply(selectedPatch(review,new Set(wanted.map(file=>file.id))));
    assert.deepEqual(await readFile(join(directory,"binary.bin")),Buffer.from([0,7,8,9]));assert.equal(await readFile(join(directory,"new name.txt"),"utf8"),"same\n");assert.equal(await readFile(join(directory,"deleted.txt"),"utf8"),"remove\n");
  });
});

test("CRLF content and no-final-newline markers survive partial selection", async () => {
  for (const newline of ["\n", "\r\n"]) await fixture({"中文 文件.txt": Array.from({length:25},(_,i)=>`第${i}行`).join(newline)},async(directory,git,apply)=>{
    const before=await readFile(join(directory,"中文 文件.txt"),"utf8");await writeFile(join(directory,"中文 文件.txt"),before.replace("第1行","first").replace("第24行","last"));
    const patch=git("-c","core.quotePath=false","diff","--binary","--no-ext-diff","--no-color");git("checkout","--",".");
    const review=reviewPatch(patch);assert.equal(review.error,"");assert.equal(review.files[0].hunks.length,2);apply(selectedPatch(review,new Set([review.files[0].hunks[1].id])));
    assert.equal(await readFile(join(directory,"中文 文件.txt"),"utf8"),before.replace("第24行","last"));
  });
});

test("truncated, overlapping, unknown and trailing prose patches cannot produce an applicable selection", () => {
  const valid="diff --git a/a.txt b/a.txt\n--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-old\n+new\n";
  for(const patch of [valid.slice(0,-1),valid.replace("+new\n",""),valid+"unexpected prose\n",valid+"@@ -1 +1 @@\n-old\n+new\n",valid+"diff --git a/b.txt b/b.txt\n",valid.replace("@@ -1 +1", "@@ -1 +9"),"--- a/a.txt\n+++ b/a.txt\n@@ -1 +1 @@\n-a\n+b\n"]) {
    const review=reviewPatch(patch);assert(review.error,patch);assert.equal(review.files.length,0);assert.throws(()=>selectedPatch(review,new Set()),Error);
  }
});
