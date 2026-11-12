import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import type { ProxyOptions } from "vite";
const centerProxy: ProxyOptions = {
  target: "http://127.0.0.1:22341",
  configure(proxy) {
    proxy.on("error", (_error, _req, res) => {
      if ("writeHead" in res && !res.headersSent) {
        res.writeHead(502, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: { code: "upstream_unavailable" } }));
      }
    });
  },
};
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
    host: "127.0.0.1",
    port: 22340,
    strictPort: true,
    proxy: {
      "/api": centerProxy,
      // 安装完成后安装页按 /healthz 判断中心是否已切到正常服务
      "/healthz": centerProxy,
      // 带尾斜杠：避免把前端路由 /subscriptions/* 也代理到中心
      "/sub/": centerProxy,
    },
  },
  build: { outDir: "../weir/static", emptyOutDir: true },
});
