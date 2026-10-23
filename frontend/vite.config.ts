import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import { pdfResources } from "./pdf-resources";

export default defineConfig(({ mode }) => {
  const target = loadEnv(mode, ".", "PROOFCODE_").PROOFCODE_CONTROL_PLANE_URL || "http://127.0.0.1:8080";
  return { base: "./", plugins: [react(), pdfResources()], server: { proxy: { "/api": { target }, "/ws": { target, ws: true } } } };
});
