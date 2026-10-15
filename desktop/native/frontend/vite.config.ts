import { defineConfig, searchForWorkspaceRoot } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

const nativeModule = (name: string) => fileURLToPath(new URL(`./node_modules/${name}`, import.meta.url));
export default defineConfig({
  base: "./",
  plugins: [react()],
  resolve: { alias: { "react": nativeModule("react"), "react-dom": nativeModule("react-dom"), "lucide-react": nativeModule("lucide-react"), "monaco-editor": nativeModule("monaco-editor"), "react-markdown": nativeModule("react-markdown"), "remark-gfm": nativeModule("remark-gfm") } },
  server: { fs: { allow: [searchForWorkspaceRoot(process.cwd()), fileURLToPath(new URL("../../../frontend", import.meta.url))] } }
});
