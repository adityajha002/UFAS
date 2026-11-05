/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Dev: the Go admin listener runs on 127.0.0.1:9090 (admin.listen in config.yaml).
// Proxying keeps the browser on one origin, so no CORS is needed.
export default defineConfig({
  plugins: [react()],
  server: { proxy: { "/admin/api": "http://127.0.0.1:9090" } },
  build: { outDir: "dist", emptyOutDir: true, sourcemap: false },
  test: { environment: "jsdom", globals: true },
});
