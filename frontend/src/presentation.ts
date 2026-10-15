import type { DataPlan } from "./dataModel";

export const taskLabel = (status: string) => ({ SUCCEEDED: "已完成", VERIFYING: "验证中", RUNNING: "运行中", QUEUED: "排队中", WAITING_APPROVAL: "待审批", FAILED: "失败", CANCELLED: "已取消" }[status] || status);
export const environmentLabel = (value: string) => ({ dev: "开发环境", staging: "预发布环境", production: "生产环境" }[value] || value);
export const auditLabel = (value: string) => ({ PLAN_CREATED: "生成计划", USER_APPROVED: "用户批准", USER_DENIED: "用户拒绝", EXECUTION_STARTED: "开始执行", EXECUTION_FINISHED: "执行结束", COMPENSATION_PLANNED: "生成恢复计划" }[value] || value);
export const toolLabel = (value: string) => ({ read_file: "读取文件", list_files: "浏览文件", search_code: "搜索代码", git_diff: "检查代码差异", apply_patch: "修改文件", run_command: "运行命令", context_read: "回取上下文", code_search: "检索代码证据", data_execute: "执行数据操作", browser: "浏览器验证" }[value] || value);
export const riskLabel = (risk: string) => ({ low: "低风险", medium: "中风险", high: "高风险", critical: "极高风险", read: "只读", write: "写入", execute: "执行" }[risk.toLowerCase()] || `风险级别：${risk || "未提供"}`);
export function planTitle(plan: DataPlan): string {
  if (plan.kind === "compensation") return "按原始快照恢复数据";
  if (plan.kind === "migration") return `调整表结构 · ${Array.isArray(plan.ir.steps) ? plan.ir.steps.length : 0} 项变更`;
  if (plan.kind === "cache") return `${({ scan: "浏览键空间", get: "读取键值", set: "写入键值", delete: "删除键", expire: "调整有效期" } as Record<string, string>)[String(plan.ir.operation)] || "缓存操作"}${plan.ir.key ? ` · ${String(plan.ir.key)}` : ""}`;
  return `${plan.kind === "query" ? "查询" : plan.ir.operation === "delete" ? "删除" : "更新"} ${String(plan.ir.schema || "")}.${String(plan.ir.table || "")}`;
}
export function planProgress(status: string) {
  const accepted = ["APPROVED", "EXECUTING", "SUCCEEDED", "COMMITTED", "FAILED_ROLLED_BACK", "FAILED_PRECONDITION", "COMMIT_UNKNOWN", "COMPENSATED", "COMPENSATION_CONFLICT", "COMPENSATION_UNKNOWN"].includes(status);
  const completed = ["SUCCEEDED", "COMMITTED", "COMPENSATED"].includes(status);
  const failed = ["FAILED_ROLLED_BACK", "FAILED_PRECONDITION", "COMPENSATION_CONFLICT"].includes(status);
  return [
    { label: "计划已生成", tone: "done" },
    { label: status === "DENIED" ? "已拒绝" : accepted ? "已批准" : "等待批准", tone: status === "DENIED" ? "failed" : accepted ? "done" : "current" },
    { label: status === "DENIED" ? "未执行" : completed ? "执行完成" : status === "FAILED_ROLLED_BACK" ? "失败已回滚" : failed ? "执行失败" : status === "EXECUTING" ? "正在执行" : status.endsWith("UNKNOWN") ? "结果待核对" : "等待执行", tone: completed ? "done" : failed || status === "DENIED" ? "failed" : status === "EXECUTING" || status.endsWith("UNKNOWN") || status === "APPROVED" ? "current" : "pending" }
  ];
}
