import type { DesktopApp, LocalProject, Project } from "./types";

export const token = () => localStorage.getItem("proofcode.token") || "change-me";
export const desktopApp = () => (window as unknown as { go?: { main?: { App?: DesktopApp } } }).go?.main?.App;
export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const bridge = desktopApp()?.ProxyRequest;
  if (bridge) {
    if (init?.body != null && typeof init.body !== "string") throw new Error("Desktop API body must be JSON text");
    const headers = new Headers(init?.headers);
    const response = await bridge(path, init?.method || "GET", init?.body || "", `Bearer ${token()}`, headers.get("Idempotency-Key") || "");
    if (response.status < 200 || response.status >= 300) throw new Error(`${response.status} ${response.body}`);
    return response.status === 204 ? (undefined as T) : JSON.parse(response.body) as T;
  }
  const headers = new Headers(init?.headers);
  headers.set("Content-Type", "application/json");
  headers.set("Authorization", `Bearer ${token()}`);
  const response = await fetch(path, { ...init, headers });
  if (!response.ok) throw new Error(`${response.status} ${await response.text()}`);
  return response.status === 204 ? (undefined as T) : response.json();
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
