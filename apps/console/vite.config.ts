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
      // 带尾斜杠：避免把前端路由 /subscriptions/* 也代理到中心
      "/sub/": "http://127.0.0.1:19810",
    },
  },
  build: { outDir: "../weir/static", emptyOutDir: true },
});
