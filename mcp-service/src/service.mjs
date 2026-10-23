import { realpath, stat } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import Ajv from "ajv";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { CallToolRequestSchema, ListToolsRequestSchema } from "@modelcontextprotocol/sdk/types.js";

export async function createService(config) {
  const validators = new Ajv({ strict: true, allErrors: false, validateFormats: false });
  const tools = new Map();
  for (const entry of config.modules) {
    const path = await realpath(resolve(dirname(config.configPath), entry));
    if (![".mjs", ".js"].includes(extname(path)) || !(await stat(path)).isFile()) throw new Error("Tool module must be a local JavaScript file");
    const plugin = await import(pathToFileURL(path).href);
    if (!Array.isArray(plugin.tools) || !plugin.tools.length) throw new Error("Tool module must export a nonempty tools array");
    for (const value of plugin.tools) {
      if (!value || typeof value.name !== "string" || !/^[A-Za-z][A-Za-z0-9_-]{0,23}$/.test(value.name) || tools.has(value.name) || tools.size >= 128) throw new Error("Invalid, duplicate or excessive custom tools");
      if (typeof value.description !== "string" || !value.description.trim() || value.description.length > 2000 || typeof value.execute !== "function") throw new Error("Custom tool needs a description and execute function");
      const schema = value.inputSchema;
      if (!schema || schema.type !== "object" || JSON.stringify(schema).length > 32768 || schema.$async) throw new Error("Custom tool needs a bounded object input schema");
      tools.set(value.name, { ...value, validate: validators.compile(schema) });
    }
  }
  let active = 0;
  const controllers = new Set();
  const redact = text => config.secrets.reduce((value, secret) => value.split(secret).join("[redacted]"), String(text));
  const definitions = [...tools.values()].map(value => ({ name: value.name, description: value.description, inputSchema: value.inputSchema }));
  if (redact(JSON.stringify(definitions)) !== JSON.stringify(definitions)) throw new Error("Tool metadata contains configured credentials");
  const failure = text => ({ content: [{ type: "text", text }], isError: true });
  return {
    names: [...tools.keys()],
    close() { for (const controller of controllers) controller.abort(); },
    createServer() {
      const server = new Server({ name: config.name, version: "0.1.0" }, { capabilities: { tools: {} } });
      server.setRequestHandler(ListToolsRequestSchema, () => ({ tools: definitions }));
      server.setRequestHandler(CallToolRequestSchema, async (request, extra) => {
        const tool = tools.get(request.params.name);
        if (!tool) return failure("Unknown tool");
        const args = request.params.arguments ?? {};
        if (!tool.validate(args)) return failure("Tool arguments do not match the published input schema");
        if (active >= config.concurrency) return failure("Tool service is busy; retry after another call completes");
        active++;
        const controller = new AbortController();
        controllers.add(controller);
        const abort = () => controller.abort();
        extra.signal.addEventListener("abort", abort, { once: true });
        if (extra.signal.aborted) controller.abort();
        const timeout = setTimeout(abort, config.timeoutMs);
        const execution = Promise.resolve().then(() => { controller.signal.throwIfAborted(); return tool.execute(args, { signal: controller.signal, root: config.root }); });
        // A module that ignores abort keeps its slot until its work actually settles.
        void execution.finally(() => { controllers.delete(controller); active--; }).catch(() => {});
        let onAbort;
        try {
          const cancelled = new Promise((_, reject) => { onAbort = () => reject(new Error("cancelled")); controller.signal.addEventListener("abort", onAbort, { once: true }); if (controller.signal.aborted) onAbort(); });
          const value = await Promise.race([execution, cancelled]);
          const encoded = typeof value === "string" ? value : JSON.stringify(value);
          if (typeof encoded !== "string" || Buffer.byteLength(encoded) > 256 * 1024) return failure("Tool output exceeds 256 KiB or is not serializable");
          return { content: [{ type: "text", text: redact(encoded) }], isError: false };
        } catch {
          return failure(controller.signal.aborted ? "Tool cancelled or timed out" : "Tool execution failed; check the local tool implementation");
        } finally {
          clearTimeout(timeout);
          controller.signal.removeEventListener("abort", onAbort);
          extra.signal.removeEventListener("abort", abort);
        }
      });
      return server;
    }
  };
}
