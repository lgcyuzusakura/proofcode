import { readFile, realpath, stat } from "node:fs/promises";
import { dirname, resolve } from "node:path";

const object = value => !!value && typeof value === "object" && !Array.isArray(value);
const envName = value => typeof value === "string" && /^[A-Za-z_][A-Za-z0-9_]{0,127}$/.test(value);
const hostName = value => typeof value === "string" && value.length <= 253 && /^(?:[A-Za-z0-9.-]+|\[[a-fA-F0-9:]+\])$/.test(value);

export async function loadConfig(path, environment = process.env) {
  const configPath = await realpath(resolve(path));
  if ((await stat(configPath)).size > 64 * 1024) throw new Error("Configuration exceeds 64 KiB");
  const config = JSON.parse(await readFile(configPath, "utf8"));
  if (!object(config) || config.version !== 1 || !/^[A-Za-z][A-Za-z0-9_-]{0,31}$/.test(config.name || "")) throw new Error("Expected configuration version 1 and a service name");
  if (!Array.isArray(config.modules) || !config.modules.length || config.modules.length > 16 || config.modules.some(value => typeof value !== "string" || !value || value.length > 2048)) throw new Error("Configure between 1 and 16 local tool modules");
  if (typeof config.root !== "string" || !config.root) throw new Error("Configure an explicit workspace root");
  const root = await realpath(resolve(dirname(configPath), config.root));
  if (!(await stat(root)).isDirectory()) throw new Error("Workspace root must be a directory");
  const timeoutMs = config.timeoutMs ?? 15000, concurrency = config.concurrency ?? 4;
  if (!Number.isInteger(timeoutMs) || timeoutMs < 100 || timeoutMs > 60000 || !Number.isInteger(concurrency) || concurrency < 1 || concurrency > 8) throw new Error("Invalid tool timeout or concurrency limit");
  const secretEnv = config.secretEnv ?? [];
  if (!Array.isArray(secretEnv) || secretEnv.length > 32 || secretEnv.some(value => !envName(value))) throw new Error("Invalid secret environment variable names");
  const transport = config.transport ?? { type: "stdio" };
  if (!object(transport) || !["stdio", "http"].includes(transport.type)) throw new Error("Transport must be stdio or http");
  let token = "";
  if (transport.type === "http") {
    if (!hostName(transport.host) || !Number.isInteger(transport.port) || transport.port < 0 || transport.port > 65535 || !envName(transport.tokenEnv)) throw new Error("Invalid HTTP listener configuration");
    token = environment[transport.tokenEnv] || "";
    if (token.length < 32 || token.length > 4096 || /[^\x21-\x7e]/.test(token)) throw new Error("Set the HTTP token environment variable to 32-4096 printable characters");
    if (!Array.isArray(transport.allowedHosts) || !transport.allowedHosts.length || transport.allowedHosts.length > 32 || transport.allowedHosts.some(value => !hostName(value) || ["0.0.0.0", "[::]"].includes(value))) throw new Error("Configure explicit HTTP host names");
    if (!Array.isArray(transport.allowedOrigins) || transport.allowedOrigins.length > 32 || transport.allowedOrigins.some(value => { try { const url = new URL(value); return !["http:", "https:"].includes(url.protocol) || url.origin !== value; } catch { return true; } })) throw new Error("Configure explicit HTTP origins, or an empty list");
  }
  const secrets = [...new Set([token, ...secretEnv.map(name => environment[name] || "")].filter(Boolean))].sort((a, b) => b.length - a.length);
  return { ...config, configPath, root, timeoutMs, concurrency, transport, token, secrets };
}
