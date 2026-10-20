import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      cn: new URL(
        "../../packages/ui/src/cn.ts",
        import.meta.url,
      ).pathname.replace(/^\/(\w:)/, "$1"),
    },
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:19810",
      "/sub": "http://127.0.0.1:19810",
    },
  },
  build: { outDir: "../control/static", emptyOutDir: true },
});
