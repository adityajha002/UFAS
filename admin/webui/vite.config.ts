/// <reference types="vitest/config" />
import fs from "node:fs";
import path from "node:path";
import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";

function readAppCloudflareUrl(): string | null {
  for (const rel of ["../../../app/.env", "../../app/.env"]) {
    try {
      const p = path.resolve(import.meta.dirname, rel);
      const raw = fs.readFileSync(p, "utf8");
      for (const line of raw.split(/\r?\n/)) {
        const m = /^\s*EXPO_PUBLIC_API_BASE_URL\s*=\s*(.+?)\s*$/.exec(line);
        if (m && /^https:\/\//i.test(m[1])) {
          return m[1].replace(/\/+$/, "");
        }
      }
    } catch {
      // ignore missing file
    }
  }
  return null;
}

// Dev: proxy /admin/api to VITE_API_BASE_URL / VITE_PROXY_TARGET, or the HTTPS
// Cloudflare URL from app/.env if present, falling back to local 127.0.0.1:9090.
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const proxyTarget = (
    env.VITE_PROXY_TARGET ||
    env.VITE_API_BASE_URL ||
    readAppCloudflareUrl() ||
    "http://127.0.0.1:9090"
  ).replace(/\/+$/, "");

  return {
    plugins: [react()],
    server: {
      proxy: {
        "/admin/api": {
          target: proxyTarget,
          changeOrigin: true,
          secure: true,
          headers: { "ngrok-skip-browser-warning": "1" },
        },
        "/server/shutdown": {
          target: proxyTarget,
          changeOrigin: true,
          secure: true,
          headers: { "ngrok-skip-browser-warning": "1" },
        },
      },
    },
    build: { outDir: "dist", emptyOutDir: true, sourcemap: false },
    test: { environment: "jsdom", globals: true },
  };
});
