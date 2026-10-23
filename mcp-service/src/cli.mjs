import { parseArgs } from "node:util";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { loadConfig } from "./config.mjs";
import { createService } from "./service.mjs";
import { listenHTTP } from "./http.mjs";

try {
  const { values } = parseArgs({ options: { config: { type: "string" }, check: { type: "boolean", default: false } }, allowPositionals: false });
  if (!values.config) throw new Error("Pass --config with a trusted local configuration path");
  const config = await loadConfig(values.config), service = await createService(config);
  if (values.check) {
    process.stderr.write(`${JSON.stringify({ status: "valid", service: config.name, tools: service.names, transport: config.transport.type })}\n`);
  } else {
    const endpoint = config.transport.type === "http" ? await listenHTTP(config, service) : service.createServer();
    if (config.transport.type === "stdio") await endpoint.connect(new StdioServerTransport());
    process.stderr.write(`${JSON.stringify({ status: "ready", service: config.name, transport: config.transport.type, url: endpoint.url, tools: service.names })}\n`);
    let closing = false;
    const close = async () => { if (closing) return; closing = true; service.close(); await endpoint.close(); process.exitCode = 0; };
    process.once("SIGINT", () => void close());
    process.once("SIGTERM", () => void close());
    if (config.transport.type === "stdio") process.stdin.once("end", () => void close());
  }
} catch {
  process.stderr.write("MCP service could not start. Check the configuration, module schemas, dependencies and required environment variables.\n");
  process.exitCode = 1;
}
