import { createHash, randomUUID, timingSafeEqual } from "node:crypto";
import { createServer } from "node:http";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { isInitializeRequest } from "@modelcontextprotocol/sdk/types.js";

const digest = value => createHash("sha256").update(value).digest();

export async function listenHTTP(config, service) {
  const expected = digest(`Bearer ${config.token}`), sessions = new Map(), connections = new Set();
  let initializing = 0;
  const listener = createServer(async (request, response) => {
    response.setHeader("Cache-Control", "no-store");
    response.setHeader("X-Content-Type-Options", "nosniff");
    const reject = (status, message) => { response.writeHead(status, { "Content-Type": "application/json" }); response.end(JSON.stringify({ error: message })); };
    let authority;
    try { authority = new URL(`http://${request.headers.host}`); if (!request.headers.host || authority.host !== request.headers.host.toLowerCase()) throw new Error(); } catch { return reject(400, "Invalid Host"); }
    if (!config.transport.allowedHosts.some(value => value.toLowerCase() === authority.hostname.toLowerCase())) return reject(403, "Host is not allowed");
    if (request.headers.origin && !config.transport.allowedOrigins.includes(request.headers.origin)) return reject(403, "Origin is not allowed");
    const authorization = request.headers.authorization || "";
    if (authorization.length > 4103 || !timingSafeEqual(digest(authorization), expected)) return reject(401, "Bearer authorization is required");
    if (request.url !== "/mcp") return reject(404, "Not found");
    if (!["POST", "DELETE"].includes(request.method)) { response.setHeader("Allow", "POST, DELETE"); return reject(405, "This MCP endpoint accepts POST and DELETE requests"); }
    const sessionId = request.headers["mcp-session-id"];
    if (sessionId && (typeof sessionId !== "string" || !/^[a-f0-9-]{36}$/.test(sessionId))) return reject(400, "Invalid session ID");
    let session = sessionId ? sessions.get(sessionId) : undefined;
    if (sessionId && !session) return reject(404, "Session not found");
    if (request.method === "DELETE") {
      if (!session) return reject(400, "Session ID is required");
      try { await session.transport.handleRequest(request, response); } catch { if (!response.headersSent) reject(500, "Session close failed"); }
      return;
    }
    if (!(request.headers["content-type"] || "").toLowerCase().startsWith("application/json")) return reject(415, "JSON content type is required");
    let fresh = false;
    try {
      const chunks = []; let size = 0;
      for await (const chunk of request.iterator({ destroyOnReturn: false })) { size += chunk.length; if (size > 1024 * 1024) { response.setHeader("Connection", "close"); response.once("finish", () => request.destroy()); request.resume(); reject(413, "Request exceeds 1 MiB"); return; } chunks.push(chunk); }
      let body;
      try { body = JSON.parse(Buffer.concat(chunks).toString("utf8")); } catch { return reject(400, "Invalid JSON"); }
      if (!body || typeof body !== "object" || Array.isArray(body)) return reject(400, "A single JSON-RPC object is required");
      if (!session) {
        if (!isInitializeRequest(body)) return reject(400, "Initialize an MCP session first");
        if (sessions.size + initializing >= 64) return reject(503, "Too many open MCP sessions");
        initializing++; fresh = true;
        const server = service.createServer();
        const transport = new StreamableHTTPServerTransport({ sessionIdGenerator: () => randomUUID(), enableJsonResponse: true, onsessioninitialized: id => sessions.set(id, session) });
        session = { server, transport, open: 0, lastActive: Date.now(), pending: new Map() };
        connections.add(server);
        server.onclose = () => { connections.delete(server); if (transport.sessionId) sessions.delete(transport.sessionId); };
        await server.connect(transport);
      }
      const requestKey = body.id === undefined ? undefined : JSON.stringify(body.id);
      if (requestKey && session.pending.has(requestKey)) return reject(409, "Duplicate active request ID");
      if (requestKey) session.pending.set(requestKey, response);
      session.open++; session.lastActive = Date.now();
      response.once("close", () => {
        session.open--; session.lastActive = Date.now();
        if (requestKey) session.pending.delete(requestKey);
        // Route a dropped HTTP request to the SDK's normal cooperative cancellation path.
        if (!response.writableFinished && requestKey) session.transport.onmessage?.({ jsonrpc: "2.0", method: "notifications/cancelled", params: { requestId: body.id, reason: "HTTP request disconnected" } });
      });
      const cancelledResponse = body.method === "notifications/cancelled" ? session.pending.get(JSON.stringify(body.params?.requestId)) : undefined;
      await session.transport.handleRequest(request, response, body);
      // An aborted SDK request intentionally emits no result; release its HTTP response too.
      if (cancelledResponse && !cancelledResponse.writableEnded) cancelledResponse.destroy();
      if (fresh && !session.transport.sessionId) await session.server.close();
    } catch {
      if (!response.headersSent && !response.destroyed) reject(500, "MCP request failed");
      else if (!response.writableEnded) response.end();
      if (fresh && session) await session.server.close().catch(() => {});
    } finally {
      if (fresh) initializing--;
    }
  });
  const expire = setInterval(() => {
    const cutoff = Date.now() - 10 * 60 * 1000;
    for (const session of sessions.values()) if (session.open === 0 && session.lastActive < cutoff) void session.server.close().catch(() => {});
  }, 60000);
  expire.unref();
  listener.requestTimeout = 15000;
  listener.headersTimeout = 10000;
  listener.maxConnections = 64;
  try { await new Promise((resolve, reject) => { listener.once("error", reject); listener.listen(config.transport.port, config.transport.host.replace(/^\[|\]$/g, ""), resolve); }); }
  catch (error) { clearInterval(expire); throw error; }
  const address = listener.address();
  return {
    url: `http://${config.transport.host}:${address.port}/mcp`,
    async close() {
      service.close();
      clearInterval(expire);
      await Promise.allSettled([...connections].map(server => server.close()));
      listener.closeAllConnections();
      await new Promise(resolve => listener.close(resolve));
    }
  };
}
