import { defineConfig, searchForWorkspaceRoot } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

const nativeModule = (name: string) => fileURLToPath(new URL(`./node_modules/${name}`, import.meta.url));
export default defineConfig({
  base: "./",
  plugins: [react()],
  resolve: { alias: { "react": nativeModule("react"), "react-dom": nativeModule("react-dom"), "lucide-react": nativeModule("lucide-react") } },
  server: { fs: { allow: [searchForWorkspaceRoot(process.cwd()), fileURLToPath(new URL("../../../frontend", import.meta.url))] } }
});
