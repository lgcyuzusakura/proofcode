import { defineConfig, searchForWorkspaceRoot } from "vite";
import react from "@vitejs/plugin-react";
import { pdfResources } from "../../../frontend/pdf-resources";
import { fileURLToPath } from "node:url";

const nativeModule = (name: string) => fileURLToPath(new URL(`./node_modules/${name}`, import.meta.url));
export default defineConfig({
  base: "./",
  plugins: [react(), pdfResources()],
  resolve: { alias: { "react": nativeModule("react"), "react-dom": nativeModule("react-dom"), "lucide-react": nativeModule("lucide-react"), "monaco-editor": nativeModule("monaco-editor"), "react-markdown": nativeModule("react-markdown"), "remark-gfm": nativeModule("remark-gfm"), "diff": nativeModule("diff"), "docx-preview": nativeModule("docx-preview"), "jszip": nativeModule("jszip"), "pdfjs-dist": nativeModule("pdfjs-dist") } },
  server: { fs: { allow: [searchForWorkspaceRoot(process.cwd()), fileURLToPath(new URL("../../../frontend", import.meta.url))] } }
});
