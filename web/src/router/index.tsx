import { createBrowserRouter, Navigate, Outlet, useLocation } from "react-router";

import { WsRuntime } from "@/components/ws-runtime";
import AdminAiLayout from "@/pages/admin-ai/layout";
import ChannelsPage from "@/pages/admin-ai/channels";
import ModelsPage from "@/pages/admin-ai/models";
import PluginsPage from "@/pages/admin-ai/plugins";
import Canvas from "@/pages/canvas";
import CanvasList from "@/pages/canvas-list";
import Login from "@/pages/login";
import { getToken } from "@/utils/storage/token";

function RequireAuth() {
  const location = useLocation();

  if (!getToken()) {
    const next = `${location.pathname}${location.search}${location.hash}`;
    return <Navigate to={`/login?next=${encodeURIComponent(next)}`} replace />;
  }

  return (
    <>
      {/* 登录态下常驻：任务 WebSocket 在列表页、画布页之间不断线 */}
      <WsRuntime />
      <Outlet />
    </>
  );
}

export const router = createBrowserRouter([
  {
    path: "/login",
    element: <Login />,
  },
  {
    element: <RequireAuth />,
    children: [
      {
        index: true,
        element: <CanvasList />,
      },
      {
        path: "canvas",
        element: <Navigate to="/" replace />,
      },
      {
        path: "canvas/:id",
        element: <Canvas />,
      },
      {
        path: "admin/ai",
        element: <AdminAiLayout />,
        children: [
          { index: true, element: <Navigate to="models" replace /> },
          { path: "models", element: <ModelsPage /> },
          { path: "models/new", element: <ModelsPage /> },
          { path: "channels", element: <ChannelsPage /> },
          { path: "plugins", element: <PluginsPage /> },
        ],
      },
    ],
  },
]);
