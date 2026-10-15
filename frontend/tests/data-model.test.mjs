import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import ts from "typescript";

const source = await readFile(new URL("../src/dataModel.ts", import.meta.url), "utf8");
const javascript = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const { filterValue, compileWhere, compileQuery, rowMutation, queryPage, compileCache, resourceRequest, recoveryAvailable, sessionActive } = await import(`data:text/javascript;base64,${Buffer.from(javascript).toString("base64")}`);

const column = (name, jdbcType, type, nullable = true, generated = false) => ({ name, jdbcType, type, nullable, generated, defaultValue: null });
const table = {
  schema: "public", name: "items", primaryKey: ["id"], foreignKeys: [], indexes: [],
  columns: [column("id", -5, "int8", false), column("code", 12, "text", false), column("amount", 2, "numeric"), column("enabled", 16, "bool", false), column("created", 12, "text", false, true)]
};
const draft = { selected: table.columns.map((value) => value.name), groups: [], connective: "and", orderBy: [], limit: 50, offset: 0 };
const filter = (name, operator, value) => [{ id: "group", operator: "and", filters: [{ id: "condition", column: name, operator, value }] }];
const hash = "a".repeat(64);
const row = { id: "9223372036854775807", code: "001", amount: "12345678901234567890.1200", enabled: true, created: "origin" };

test("PostgreSQL connection sends explicit schema allowlist and Redis an empty list", () => {
  const value = { name: "database", provider: "postgres", environment: "dev", secretRef: "PG_DEV", allowedSchema: "public", active: true };
  assert.deepEqual(resourceRequest(value), { name: "database", provider: "postgres", environment: "dev", secretRef: "PG_DEV", allowedSchema: ["public"] });
  assert.deepEqual(resourceRequest({ ...value, allowedSchema: "public, reporting" }).allowedSchema, ["public", "reporting"]);
  assert.throws(() => resourceRequest({ ...value, allowedSchema: "public, public" }));
  assert.throws(() => resourceRequest({ ...value, allowedSchema: "" }));
  assert.deepEqual(resourceRequest({ ...value, provider: "redis", secretRef: "REDIS_DEV" }), { name: "database", provider: "redis", environment: "dev", secretRef: "REDIS_DEV", allowedSchema: [] });
  assert.deepEqual(resourceRequest({ ...value, secretRef: "", active: false }, { resourceVersion: 3 }), { name: "database", environment: "dev", allowedSchema: ["public"], expectedVersion: 3, active: false });
  assert.throws(() => resourceRequest({ ...value, secretRef: "" }));
});

test("text identifiers and decimal scale survive typed filters", () => {
  assert.equal(filterValue(table.columns[1], "001"), "001");
  assert.equal(filterValue(table.columns[2], "12345678901234567890.1200"), "12345678901234567890.1200");
  assert.equal(filterValue(table.columns[3], "false"), false);
  assert.throws(() => filterValue(table.columns[3], "0"));
});

test("BIGINT filters preserve int64 boundaries and reject overflow", () => {
  assert.equal(filterValue(table.columns[0], "42"), 42);
  assert.equal(filterValue(table.columns[0], "9223372036854775807"), "9223372036854775807");
  assert.equal(filterValue(table.columns[0], "-9223372036854775808"), "-9223372036854775808");
  assert.throws(() => filterValue(table.columns[0], "9223372036854775808"));
  assert.throws(() => filterValue(table.columns[0], "1.5"));
});

test("IN prevents rounded JSON integers and retains numeric strings", () => {
  assert.throws(() => compileWhere(table, filter("id", "in", "[9223372036854775807]"), "and"));
  assert.deepEqual(compileWhere(table, filter("id", "in", '["9223372036854775807"]'), "and").conditions[0].value, ["9223372036854775807"]);
  assert.throws(() => compileWhere(table, filter("amount", "in", "[0.12]"), "and"));
  assert.deepEqual(compileWhere(table, filter("amount", "in", '["0.1200"]'), "and").conditions[0].value, ["0.1200"]);
  assert.throws(() => compileWhere(table, filter("code", "in", "[1]"), "and"));
});

test("conditions reject unknown columns and unsupported values", () => {
  assert.throws(() => compileWhere(table, filter("secret", "eq", "a"), "and"));
  assert.throws(() => compileWhere(table, filter("id", "in", "[]"), "and"));
  assert.throws(() => compileWhere(table, filter("id", "in", "[null]"), "and"));
  assert.throws(() => compileWhere(table, filter("code", "like", "a%"), "and"));
  assert.deepEqual(compileWhere(table, filter("amount", "is_null", "ignored"), "and").conditions[0], { column: "amount", operator: "is_null" });
});

test("projection hashes require all columns and valid paging", () => {
  assert.equal(compileQuery(table, draft).includeRowHashes, true);
  assert.equal(compileQuery(table, { ...draft, selected: ["id", "code"] }).includeRowHashes, undefined);
  assert.throws(() => compileQuery(table, { ...draft, selected: ["id", "id"] }));
  assert.throws(() => compileQuery(table, { ...draft, selected: ["missing"] }));
  assert.throws(() => compileQuery(table, { ...draft, limit: 1001 }));
  assert.throws(() => compileQuery(table, { ...draft, offset: -1 }));
  assert.throws(() => compileQuery(table, { ...draft, orderBy: [{ column: "id", direction: "invalid" }] }));
});

test("row mutation binds exact original primary key, hash and row count", () => {
  const ir = rowMutation(table, row, { ...row, amount: "12345678901234567890.1300" }, hash);
  assert.deepEqual(ir.filters, [{ column: "id", operator: "eq", value: "9223372036854775807" }]);
  assert.deepEqual(ir.values, { amount: "12345678901234567890.1300" });
  assert.equal(ir.expectedRows, 1);
  assert.equal(ir.expectedBeforeHash, hash);
  assert.equal(rowMutation(table, row, null, hash).operation, "delete");
});

test("row mutation refuses unreliable keys, hashes and protected changes", () => {
  assert.throws(() => rowMutation(table, row, { ...row }, hash));
  assert.throws(() => rowMutation(table, row, null, "invalid"));
  assert.throws(() => rowMutation({ ...table, primaryKey: [] }, row, null, hash));
  assert.throws(() => rowMutation(table, { ...row, id: Number.MAX_SAFE_INTEGER + 1 }, null, hash));
  assert.throws(() => rowMutation(table, row, { ...row, id: 7 }, hash));
  assert.throws(() => rowMutation(table, row, { ...row, created: "changed" }, hash));
  assert.throws(() => rowMutation(table, row, { ...row, code: null }, hash));
  assert.throws(() => rowMutation(table, row, { ...row, amount: 0.13 }, hash));
  assert.throws(() => rowMutation(table, row, { ...row, missing: "value" }, hash));
});

test("pagination retains original approved query without mutating it", () => {
  const ir = compileQuery(table, { ...draft, groups: filter("code", "eq", "001"), orderBy: [{ column: "id", direction: "desc" }] });
  const next = queryPage(ir, 50);
  assert.deepEqual(next, { ...ir, offset: 50 });
  assert.equal(ir.offset, 0);
  assert.throws(() => queryPage({ kind: "mutation" }, 50));
  assert.throws(() => queryPage(ir, 100001));
});

test("cache drafts enforce logical key, cursor and TTL bounds", () => {
  assert.deepEqual(compileCache({ kind: "cache", operation: "scan", cursor: "0", count: 50 }), { kind: "cache", operation: "scan", cursor: "0", count: 50 });
  for (const count of [0, 101, 1.5]) assert.throws(() => compileCache({ kind: "cache", operation: "scan", cursor: "0", count }));
  assert.throws(() => compileCache({ kind: "cache", operation: "scan", cursor: "-1", count: 50 }));
  assert.throws(() => compileCache({ kind: "cache", operation: "delete", key: " " }));
  for (const ttlSeconds of [0, 86401, 1.5]) assert.throws(() => compileCache({ kind: "cache", operation: "expire", key: "k", ttlSeconds }));
  assert.throws(() => compileCache({ kind: "cache", operation: "flushall" }));
});

test("cache string size is measured in UTF8 bytes", () => {
  assert.equal(compileCache({ kind: "cache", operation: "set", key: "k", value: "a".repeat(65536), ttlSeconds: 1 }).value.length, 65536);
  assert.throws(() => compileCache({ kind: "cache", operation: "set", key: "k", value: "a".repeat(65537), ttlSeconds: 1 }));
  assert.throws(() => compileCache({ kind: "cache", operation: "set", key: "k", value: "\u4e2d".repeat(21846), ttlSeconds: 1 }));
});

test("recovery requires a committed non-migration plan with explicit evidence", () => {
  const plan = { status: "COMMITTED", kind: "mutation", resultSummary: '{"recoveryAvailable":true}' };
  assert.equal(recoveryAvailable(plan), true);
  assert.equal(recoveryAvailable({ ...plan, status: "COMMIT_UNKNOWN" }), false);
  assert.equal(recoveryAvailable({ ...plan, kind: "migration" }), false);
  assert.equal(recoveryAvailable({ ...plan, resultSummary: "invalid" }), false);
  assert.equal(sessionActive({ status: "ACTIVE", expiresAt: new Date(Date.now() + 60000).toISOString() }), true);
  assert.equal(sessionActive({ status: "ACTIVE", expiresAt: new Date(Date.now() - 1).toISOString() }), false);
});
