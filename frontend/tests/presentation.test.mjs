import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const source = await readFile(new URL("../src/presentation.ts", import.meta.url), "utf8");
const javascript = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const { planProgress, riskLabel } = await import(`data:text/javascript;base64,${Buffer.from(javascript).toString("base64")}`);

test("denied and uncertain plans never display execution completion", () => {
  const denied = planProgress("DENIED");
  assert.equal(denied[1].label, "已拒绝");
  assert.equal(denied[2].label, "未执行");
  for (const status of ["COMMIT_UNKNOWN", "COMPENSATION_UNKNOWN"]) {
    const steps = planProgress(status);
    assert.equal(steps[2].label, "结果待核对");
    assert.notEqual(steps[2].tone, "done");
  }
  const rollback = planProgress("FAILED_ROLLED_BACK");
  assert.equal(rollback[2].label, "失败已回滚");
  assert.notEqual(rollback[2].tone, "done");
});

test("unrecognized risks remain explicit rather than appearing low risk", () => {
  assert.equal(riskLabel("CRITICAL"), "极高风险");
  assert.equal(riskLabel("UNKNOWN"), "风险级别：UNKNOWN");
});
