import type { DesktopApp, LocalProject, Project } from "./types";

export class ApiError extends Error {
  constructor(public readonly status: number, body: string) {
    super(status === 401 || status === 403
      ? "账户登录已失效，请重新登录。"
      : `${status} ${body}`);
    this.name = "ApiError";
  }
}

export const token = () => localStorage.getItem("proofcode.session") || "";
export const desktopApp = () => (window as unknown as { go?: { main?: { App?: DesktopApp } } }).go?.main?.App;
export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const session = token();
  const expire = (status: number) => {
    if ((status === 401 || status === 403) && path !== "/api/auth/login" && path !== "/api/auth/register" && session === token()) {
      clearSession();
      window.dispatchEvent(new Event("proofcode:auth-expired"));
    }
  };
  const bridge = desktopApp()?.ProxyRequest;
  if (bridge) {
    if (init?.body != null && typeof init.body !== "string") throw new Error("Desktop API body must be JSON text");
    const headers = new Headers(init?.headers);
    const response = await bridge(path, init?.method || "GET", init?.body || "", session ? `Bearer ${session}` : "", headers.get("Idempotency-Key") || "");
    if (response.status < 200 || response.status >= 300) { expire(response.status); throw apiFailure(path, response.status, response.body); }
    return response.status === 204 ? (undefined as T) : JSON.parse(response.body) as T;
  }
  const headers = new Headers(init?.headers);
  headers.set("Content-Type", "application/json");
  if (session) headers.set("Authorization", `Bearer ${session}`);
  const response = await fetch(path, { ...init, headers });
  if (!response.ok) { expire(response.status); throw apiFailure(path, response.status, await response.text()); }
  return response.status === 204 ? (undefined as T) : response.json();
}
export type AccountUser = { id: string; username: string };
export type AccountLogin = { user: AccountUser; token: string; expiresAt: string };
export async function authRequest(path: string, body: unknown): Promise<AccountLogin> {
  const response = await api<AccountLogin>(path, { method: "POST", body: JSON.stringify(body) });
  localStorage.setItem("proofcode.session", response.token);
  window.dispatchEvent(new Event("proofcode:auth-changed"));
  return response;
}
export function clearSession() { localStorage.removeItem("proofcode.session"); localStorage.removeItem("proofcode.token"); window.dispatchEvent(new Event("proofcode:auth-changed")); }
export async function logout() {
  const session = token();
  try { await api("/api/auth/logout", { method: "POST", signal: AbortSignal.timeout(5000) }); }
  finally { if (session === token()) clearSession(); }
}
function apiFailure(path: string, status: number, body: string): Error {
  if (path.startsWith("/api/auth/") || path.startsWith("/api/model-providers")) {
    try { const value = JSON.parse(body) as { error?: unknown }; if (typeof value.error === "string" && value.error.length <= 500) return new Error(value.error); } catch { /* Keep unknown server diagnostics out of auth forms. */ }
    if (path.startsWith("/api/auth/")) return new Error(status === 401 ? "账户或密码不正确" : "账户服务暂不可用，请稍后重试");
  }
  return new ApiError(status, body);
}
export function post<T>(path: string, body: unknown, idempotencyKey?: string): Promise<T> {
  return api<T>(path, { method: "POST", headers: idempotencyKey ? { "Idempotency-Key": idempotencyKey } : undefined, body: JSON.stringify(body) });
}
export function scopedPath(projectId: string): string { return `/api/projects/${encodeURIComponent(projectId)}`; }

const pendingKey = "proofcode.pending-bootstrap";
type PendingBootstrap = { bootstrapId: string; title: string };
export function pendingBootstrap(): PendingBootstrap | null {
  try {
    const saved = JSON.parse(localStorage.getItem(pendingKey) || "null") as PendingBootstrap | null;
    return saved && typeof saved.bootstrapId === "string" && typeof saved.title === "string" ? saved : null;
  } catch { return null; }
}
export async function createScratchProject(title: string): Promise<Project> {
  const pending = pendingBootstrap() || { bootstrapId: crypto.randomUUID(), title: title.trim().slice(0, 120) || "未命名工程" };
  localStorage.setItem(pendingKey, JSON.stringify(pending));
  const bridge = desktopApp();
  const local = bridge?.BootstrapScratchProject ? await bridge.BootstrapScratchProject(pending.bootstrapId, pending.title) : null;
  const project = await registerProject(local, pending.title, pending.bootstrapId);
  localStorage.removeItem(pendingKey);
  return project;
}
export function registerProject(local: LocalProject | null, name: string, bootstrapId: string): Promise<Project> {
  return post<Project>("/api/projects", { name, repositoryUrl: null, defaultBranch: "main", sourceKind: local ? "LOCAL_FOLDER" : "SCRATCH", localHandle: local?.localHandle || null, bootstrapId }, bootstrapId);
}
