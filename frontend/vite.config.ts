import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
export default defineConfig({
  plugins: [vue()],
  build: { outDir: "../web", emptyOutDir: true },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:28189",
      "/healthz": "http://127.0.0.1:28189",
    },
  },
});
