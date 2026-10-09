export type DataResource = { id: string; projectId: string; name: string; provider: "postgres" | "redis"; environment: "dev" | "staging" | "production"; allowedSchemas: string; active: boolean; resourceVersion: number; policyVersion: string; createdAt: string };
export type DataSession = { id: string; projectId: string; name: string; purpose: "MANAGEMENT" | "RECOVERY"; status: string; createdAt: string; expiresAt: string };
export type ColumnMetadata = { name: string; type: string; jdbcType: number; nullable: boolean; generated: boolean; defaultValue: string | null; comment?: string };
export type TableMetadata = { schema: string; name: string; columns: ColumnMetadata[]; primaryKey: string[]; foreignKeys: { name: string; columns: string[]; referencesSchema: string; referencesTable: string; referencesColumns: string[] }[]; indexes: { name: string; unique: boolean; columns: string[] }[] };
export type SchemaSnapshot = { snapshotId: string; schemaVersion: string; resourceVersion: number; policyVersion: string; metadata: { tables?: TableMetadata[]; provider?: string; namespace?: string; namespaceVersion?: number; valueType?: string; mode?: string } };
export type DataIR = Record<string, unknown>;
export type DataPlan = { id: string; projectId: string; taskId: string | null; dataSessionId: string | null; attempt: number; connectionId: string; resourceVersion: number; policyVersion: string; digest: string; status: string; preview: string; schemaVersion: string; kind: string; ir: DataIR; resultSummary?: string; createdAt: string };
export type DataAudit = { id?: string; operationId: string; projectId: string; taskId: string | null; dataSessionId: string | null; action: string; status: string; details: string; createdAt: string };
export type Approval = { planId: string; status: string; digest: string; capability: string | null; expiresAt: string | null };
export type ExecutionResult = { planId: string; status: string; result: Record<string, unknown> };
export type FilterDraft = { id: string; column: string; operator: string; value: string };
export type FilterGroup = { id: string; operator: "and" | "or"; filters: FilterDraft[] };
export type SortDraft = { column: string; direction: "asc" | "desc" };
export type QueryDraft = { selected: string[]; groups: FilterGroup[]; connective: "and" | "or"; orderBy: SortDraft[]; limit: number; offset: number };
export type ResourceDraft = { name: string; provider: "postgres" | "redis"; environment: string; secretRef: string; allowedSchema: string; active: boolean };

export function resourceRequest(draft: ResourceDraft, current?: DataResource | null): Record<string, unknown> {
  const name = draft.name.trim(), secretRef = draft.secretRef.trim();
  if (!name || name.length > 120) throw new Error("连接名称需要 1 到 120 个字符");
  if (!["dev", "staging", "production"].includes(draft.environment)) throw new Error("连接环境无效");
  if ((!current || secretRef) && !/^[A-Z][A-Z0-9_]{0,79}$/.test(secretRef)) throw new Error("请使用有效的服务端凭据引用");
  const body: Record<string, unknown> = { name, environment: draft.environment };
  const schemas = draft.provider === "postgres" ? draft.allowedSchema.split(",").map((value) => value.trim()).filter(Boolean) : [];
  if (draft.provider === "postgres" && (!schemas.length || schemas.some((value) => !/^[A-Za-z_][A-Za-z0-9_]{0,62}$/.test(value)) || new Set(schemas).size !== schemas.length)) throw new Error("请填写合法且不重复的 schema 名称，使用逗号分隔");
  body.allowedSchema = schemas;
  if (current) { body.expectedVersion = current.resourceVersion; body.active = draft.active; if (secretRef) body.secretRef = secretRef; }
  else { body.provider = draft.provider; body.secretRef = secretRef; }
  return body;
}

export function parseJSON<T>(value: string | undefined | null, fallback: T): T { try { return JSON.parse(value || "") as T; } catch { return fallback; } }
export function formatCell(value: unknown): string { return value === null ? "NULL" : value === undefined ? "" : typeof value === "object" ? JSON.stringify(value) : String(value); }
export function tableKey(table: TableMetadata): string { return `${table.schema}.${table.name}`; }
export function tableQuery(table: TableMetadata): QueryDraft { return { selected: table.columns.map((column) => column.name), groups: [], connective: "and", orderBy: [], limit: 50, offset: 0 }; }

// Values retain the inspected column type; string identifiers such as "001" never become numbers.
export function filterValue(column: ColumnMetadata, text: string): unknown {
  if ([-7, 16].includes(column.jdbcType)) {
    if (!/^(true|false)$/.test(text.trim())) throw new Error(`${column.name} 需要 true 或 false`);
    return text.trim() === "true";
  }
  if (column.jdbcType === -5) {
    if (!/^[+-]?\d+$/.test(text.trim())) throw new Error(`${column.name} 需要十进制整数`);
    const value = BigInt(text.trim());
    if (value < -9223372036854775808n || value > 9223372036854775807n) throw new Error(`${column.name} 超出 BIGINT 范围`);
    return value > BigInt(Number.MAX_SAFE_INTEGER) || value < BigInt(Number.MIN_SAFE_INTEGER) ? value.toString() : Number(value);
  }
  if ([2, 3].includes(column.jdbcType)) {
    if (!/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(text.trim())) throw new Error(`${column.name} 需要十进制数值`);
    // PostgreSQL NUMERIC is sent as a decimal string to preserve precision and scale.
    return text.trim();
  }
  if ([-6, 5, 4, 6, 7, 8].includes(column.jdbcType)) {
    if (!text.trim()) throw new Error(`${column.name} 需要数值`);
    const value = Number(text);
    if (!Number.isFinite(value)) throw new Error(`${column.name} 数值无效`);
    if ([-6, 5, 4].includes(column.jdbcType) && !Number.isSafeInteger(value)) throw new Error(`${column.name} 需要安全整数`);
    return value;
  }
  return text;
}
export function compileWhere(table: TableMetadata, groups: FilterGroup[], connective: "and" | "or"): DataIR | undefined {
  const expressions = groups.filter((group) => group.filters.length).map((group) => ({
    operator: group.operator,
    conditions: group.filters.map((filter) => {
      const column = table.columns.find((candidate) => candidate.name === filter.column);
      if (!column) throw new Error(`条件字段 ${filter.column} 不在当前结构中`);
      if (["is_null", "is_not_null"].includes(filter.operator)) return { column: column.name, operator: filter.operator };
      if (filter.operator === "in") {
        const values: unknown = parseJSON(filter.value, null);
        if (!Array.isArray(values) || !values.length || values.length > 100 || values.some((value) => value === null || typeof value === "object")) throw new Error("IN 需要 1 到 100 个非空标量的 JSON 数组");
        const typed = values.map((value) => {
          if (typeof value === "number" && Number.isInteger(value) && !Number.isSafeInteger(value)) throw new Error("IN 中的大整数请使用十进制字符串，避免 JSON 数值精度丢失");
          if ([2, 3].includes(column.jdbcType) && typeof value !== "string") throw new Error(`${column.name} 的 IN 值需要十进制字符串以保持精度`);
          if (typeof value === "string" && ![-7, 16, -6, 5, 4, -5, 2, 3, 6, 7, 8].includes(column.jdbcType)) return value;
          const parsed = filterValue(column, String(value));
          if (typeof parsed !== typeof value && !(column.jdbcType === -5 && typeof value === "string")) throw new Error(`IN 中的值必须符合 ${column.name} 的类型 ${column.type}`);
          return parsed;
        });
        return { column: column.name, operator: filter.operator, value: typed };
      }
      if (!["eq", "ne", "lt", "lte", "gt", "gte"].includes(filter.operator)) throw new Error("不支持此条件运算符");
      return { column: column.name, operator: filter.operator, value: filterValue(column, filter.value) };
    })
  }));
  if (!expressions.length) return undefined;
  return expressions.length === 1 ? expressions[0] : { operator: connective, conditions: expressions };
}
export function compileQuery(table: TableMetadata, draft: QueryDraft): DataIR {
  const names = new Set(table.columns.map((column) => column.name));
  if (!draft.selected.length || draft.selected.length > 64 || new Set(draft.selected).size !== draft.selected.length || draft.selected.some((name) => !names.has(name))) throw new Error("查询需要 1 到 64 个当前表字段，不能重复");
  if (!Number.isInteger(draft.limit) || draft.limit < 1 || draft.limit > 1000 || !Number.isInteger(draft.offset) || draft.offset < 0 || draft.offset > 100000) throw new Error("每页 1 到 1000 行，偏移量 0 到 100000");
  if (draft.orderBy.length > 8 || new Set(draft.orderBy.map((sort) => sort.column)).size !== draft.orderBy.length || draft.orderBy.some((sort) => !names.has(sort.column) || !["asc", "desc"].includes(sort.direction))) throw new Error("排序字段必须来自当前表，最多 8 个且不能重复");
  const ir: DataIR = { kind: "query", schema: table.schema, table: table.name, select: draft.selected, limit: draft.limit, offset: draft.offset, orderBy: draft.orderBy };
  const where = compileWhere(table, draft.groups, draft.connective); if (where) ir.where = where;
  if (draft.selected.length === table.columns.length && draft.selected.every((name) => names.has(name))) ir.includeRowHashes = true;
  return ir;
}
export function rowMutation(table: TableMetadata, original: Record<string, unknown>, values: Record<string, unknown> | null, rowHash: string): DataIR {
  if (!table.primaryKey.length || !/^[a-f0-9]{64}$/.test(rowHash)) throw new Error("只有读取全部字段并取得行版本的主键表才支持行编辑");
  const filters = table.primaryKey.map((column) => {
    const value = original[column]; if (value === null || value === undefined) throw new Error("无法确定原行主键");
    if (typeof value === "number" && Number.isInteger(value) && !Number.isSafeInteger(value)) throw new Error("原行主键数值已超出安全整数，无法创建可靠行计划");
    return { column, operator: "eq", value };
  });
  const ir: DataIR = { kind: "mutation", schema: table.schema, table: table.name, operation: values ? "update" : "delete", filters, expectedRows: 1, expectedBeforeHash: rowHash };
  if (values) {
    const changed: Record<string, unknown> = {};
    for (const [name, value] of Object.entries(values)) {
      const column = table.columns.find((candidate) => candidate.name === name);
      if (!column) throw new Error(`未知字段 ${name}`);
      if (JSON.stringify(value) === JSON.stringify(original[name])) continue;
      if (column.generated || table.primaryKey.includes(name)) throw new Error(`不能通过行编辑修改 ${name}`);
      if (value === null && !column.nullable) throw new Error(`${name} 不能为空`);
      if (value !== null && typeof value === "object") throw new Error(`${name} 只支持标量`);
      if (typeof value === "number" && (!Number.isFinite(value) || Number.isInteger(value) && !Number.isSafeInteger(value))) throw new Error(`${name} 数值超出安全精度，请保留服务端返回的十进制字符串`);
      if (value !== null && [2, 3].includes(column.jdbcType) && typeof value !== "string") throw new Error(`${name} 为高精度数值，请使用十进制字符串`);
      if (value !== null && typeof value === "string" && [-5, 2, 3].includes(column.jdbcType)) filterValue(column, value);
      changed[name] = value;
    }
    if (!Object.keys(changed).length) throw new Error("没有字段发生变化");
    ir.values = changed;
  }
  return ir;
}
export function queryPage(ir: DataIR, offset: number): DataIR {
  if (ir.kind !== "query" || !Number.isInteger(offset) || offset < 0 || offset > 100000) throw new Error("只能对原查询生成有效分页计划");
  return { ...ir, offset };
}
export function compileCache(ir: DataIR): DataIR {
  if (ir.kind !== "cache" || !["scan", "get", "set", "delete", "expire"].includes(String(ir.operation))) throw new Error("不支持此缓存操作");
  if (ir.operation === "scan") {
    if (typeof ir.cursor !== "string" || !/^\d{1,20}$/.test(ir.cursor) || typeof ir.count !== "number" || !Number.isInteger(ir.count) || ir.count < 1 || ir.count > 100) throw new Error("SCAN 需要有效游标和 1 到 100 的 COUNT");
  } else if (typeof ir.key !== "string" || !ir.key.trim() || ir.key.length > 200) throw new Error("逻辑键需要 1 到 200 个字符");
  if (ir.operation === "set" && (typeof ir.value !== "string" || new TextEncoder().encode(ir.value).length > 65536)) throw new Error("字符串值不能超过 64 KiB");
  if (["set", "expire"].includes(String(ir.operation)) && (typeof ir.ttlSeconds !== "number" || !Number.isInteger(ir.ttlSeconds) || ir.ttlSeconds < 1 || ir.ttlSeconds > 86400)) throw new Error("TTL 需要 1 到 86400 秒");
  return ir;
}
export function sessionActive(session: DataSession): boolean { return session.status === "ACTIVE" && new Date(session.expiresAt).getTime() > Date.now(); }
export function recoveryAvailable(plan: DataPlan): boolean { return plan.status === "COMMITTED" && plan.kind !== "migration" && parseJSON<Record<string, unknown>>(plan.resultSummary, {}).recoveryAvailable === true; }
