import { lstat, opendir, readFile, realpath } from "node:fs/promises";
import { extname, join, relative, sep } from "node:path";

export const tools = [{
  name: "knowledge_search",
  description: "Search the service owner's selected local Markdown/text knowledge directory. Returns file and line evidence; does not search the rest of the computer.",
  inputSchema: { type: "object", properties: { query: { type: "string", minLength: 2, maxLength: 200 }, limit: { type: "integer", minimum: 1, maximum: 8 } }, required: ["query"], additionalProperties: false },
  async execute({ query, limit = 5 }, { root, signal }) {
    const results = [], queue = [{ path: root, depth: 0 }]; let files = 0, bytes = 0, entries = 0, complete = true;
    const terms = [...new Set(query.toLocaleLowerCase().trim().split(/\s+/).filter(Boolean))];
    if (!terms.length) return { results: [], complete: true };
    while (queue.length) {
      signal.throwIfAborted();
      const current = queue.shift();
      const directory = await opendir(current.path);
      for await (const entry of directory) {
        signal.throwIfAborted();
        if (++entries > 5000) { complete = false; break; }
        if (entry.name.startsWith(".") || ["thesis", "node_modules", "backups", "vendor"].includes(entry.name.toLowerCase()) || entry.isSymbolicLink()) continue;
        const path = join(current.path, entry.name);
        if (entry.isDirectory()) { if (current.depth < 5) queue.push({ path, depth: current.depth + 1 }); else complete = false; continue; }
        if (!entry.isFile() || ![".md", ".txt"].includes(extname(entry.name).toLowerCase())) continue;
        if (++files > 200 || bytes > 4 * 1024 * 1024) { complete = false; break; }
        const canonical = await realpath(path), rel = relative(root, canonical);
        if (rel === ".." || rel.startsWith(`..${sep}`) || (await lstat(path)).isSymbolicLink()) continue;
        const info = await lstat(path); if (info.size > 64 * 1024) { complete = false; continue; }
        const text = await readFile(path, { encoding: "utf8", signal }); bytes += Buffer.byteLength(text);
        if (text.includes("\0")) continue;
        const lines = text.split(/\r?\n/);
        for (let i = 0; i < lines.length; i++) {
          const normalized = lines[i].toLocaleLowerCase(), score = terms.filter(term => normalized.includes(term)).length;
          if (score) results.push({ path: rel.split(sep).join("/"), line: i + 1, score, text: lines.slice(Math.max(0, i - 1), i + 2).join("\n").slice(0, 1500) });
        }
        results.sort((a, b) => b.score - a.score || a.path.localeCompare(b.path) || a.line - b.line); results.length = Math.min(results.length, limit);
      }
      if (!complete && (entries > 5000 || files > 200 || bytes > 4 * 1024 * 1024)) break;
    }
    return { results, complete, filesScanned: Math.min(files, 200) };
  }
}];
