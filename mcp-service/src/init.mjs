import { randomBytes } from "node:crypto";
import { copyFile, lstat, mkdir, readFile, realpath, rename, rm, writeFile } from "node:fs/promises";
import { basename, dirname, join, resolve } from "node:path";
import { parseArgs } from "node:util";
import { fileURLToPath } from "node:url";

const source = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const files = ["package.json", "package-lock.json", ".gitignore", "README.md", "src/config.mjs", "src/service.mjs", "src/http.mjs", "src/cli.mjs", "src/init.mjs", "tools/knowledge.mjs", "knowledge/.gitkeep", "tests/service.test.mjs", "templates/custom.mjs"];

export async function scaffold(directory, transport = "http", name = "my_tools") {
  if (!["http", "stdio"].includes(transport) || !/^[A-Za-z][A-Za-z0-9_-]{0,31}$/.test(name)) throw new Error("Invalid service name or transport");
  const requested = resolve(directory), parent = await realpath(dirname(requested));
  const target = join(parent, basename(requested));
  try { await lstat(target); throw new Error("Destination already exists; choose a new directory"); } catch (error) { if (error.code !== "ENOENT") throw error; }
  // Build beside the destination, then rename only after the complete scaffold exists.
  const staging = join(parent, `.proofcode-mcp-init-${randomBytes(12).toString("hex")}`);
  await mkdir(staging, { mode: 0o700 });
  try {
    for (const path of files) { const destination = join(staging, path); await mkdir(dirname(destination), { recursive: true }); await copyFile(join(source, path), destination); }
    const config = JSON.parse(await readFile(join(source, "config.example.json"), "utf8"));
    config.name = name; config.modules.push("./tools/custom.mjs");
    if (transport === "stdio") config.transport = { type: "stdio" };
    await writeFile(join(staging, "config.local.json"), JSON.stringify(config, null, 2) + "\n", { mode: 0o600, flag: "wx" });
    await writeFile(join(staging, "config.example.json"), JSON.stringify(config, null, 2) + "\n", { flag: "wx" });
    await writeFile(join(staging, ".env"), `PROOFCODE_MCP_TOKEN=${randomBytes(32).toString("hex")}\n`, { mode: 0o600, flag: "wx" });
    await copyFile(join(source, "templates/custom.mjs"), join(staging, "tools/custom.mjs"));
    await writeFile(join(staging, "knowledge/welcome.md"), `# ${name}\n\nReplace this example with your own project documentation.\nThe default knowledge tool searches only Markdown and text files in this directory.\n`, { flag: "wx" });
    // Recheck to avoid replacing a directory created while preparing the scaffold.
    try { await lstat(target); throw new Error("Destination appeared while creating the service"); } catch (error) { if (error.code !== "ENOENT") throw error; }
    await rename(staging, target);
    return target;
  } catch (error) { await rm(staging, { recursive: true, force: true }); throw error; }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const { values } = parseArgs({ options: { directory: { type: "string" }, transport: { type: "string", default: "http" }, name: { type: "string", default: "my_tools" } }, allowPositionals: false });
    if (!values.directory) throw new Error("Pass --directory with a new service directory");
    const directory = await scaffold(values.directory, values.transport, values.name);
    process.stdout.write(`${JSON.stringify({ status: "created", directory, transport: values.transport, next: ["npm ci", "npm run check", "npm start"], credentials: "HTTP token is in the local .env file; keep it private" })}\n`);
  } catch { process.stderr.write("Unable to create the service. Choose a new directory with an existing parent, a valid name and http/stdio transport. Existing files are not replaced.\n"); process.exitCode = 1; }
}
