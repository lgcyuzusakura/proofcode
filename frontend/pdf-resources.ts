import { createReadStream } from "node:fs";
import { cp, stat } from "node:fs/promises";
import { resolve } from "node:path";
import type { Plugin } from "vite";

// Serve and package the same local PDF.js resources in Web and Wails builds.
export function pdfResources(): Plugin {
  let root = "", output = "";
  const directories = ["cmaps", "standard_fonts", "wasm"];
  return {
    name: "proofcode-pdf-resources",
    configResolved(config) { root = config.root; output = resolve(root, config.build.outDir); },
    configureServer(server) {
      server.middlewares.use(async (request, response, next) => {
        const match = request.url?.split("?")[0].match(/^\/pdfjs\/(cmaps|standard_fonts|wasm)\/([a-zA-Z0-9_.-]+)$/);
        if (!match || !["GET", "HEAD"].includes(request.method || "")) return next();
        const file = resolve(root, "node_modules/pdfjs-dist", match[1], match[2]);
        try {
          if (!(await stat(file)).isFile()) return next();
          response.setHeader("Content-Type", file.endsWith(".wasm") ? "application/wasm" : "application/octet-stream");
          if (request.method === "HEAD") return response.end();
          createReadStream(file).on("error", () => response.destroy()).pipe(response);
        } catch { next(); }
      });
    },
    async writeBundle() { await Promise.all(directories.map(name => cp(resolve(root, "node_modules/pdfjs-dist", name), resolve(output, "pdfjs", name), { recursive: true }))); },
  };
}
