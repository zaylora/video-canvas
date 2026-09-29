import path from "path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// 开发时把 /api、/files 转给后端；后端不在本机 8080 时用 VITE_PROXY_TARGET 覆盖
const proxyTarget = process.env.VITE_PROXY_TARGET ?? "http://localhost:8080";

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      // 开发时 VITE_API_BASE_URL 用相对地址 /api/v1，请求与 WebSocket 都走这条代理，
      // 浏览器视角下始终同源：不用后端放开 CORS 头（比如 Idempotency-Key），WebSocket 的 Origin 校验也天然通过。
      // 注意不要开 changeOrigin：后端 CheckOrigin 比较的是 Origin 与 Host，改写 Host 会让同源判断失败。
      "/api": { target: proxyTarget, ws: true },
      // 本地存储的素材 / 产物 URL 是相对路径 /files/...，需要转给后端
      "/files": { target: proxyTarget, changeOrigin: true },
    },
  },
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
});
