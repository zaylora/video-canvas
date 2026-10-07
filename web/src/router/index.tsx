import { createBrowserRouter, Navigate, Outlet, useLocation } from "react-router";

import { DialogHost } from "@/components/dialog-host";
import { WsRuntime } from "@/components/ws-runtime";
import AdminLayout from "@/pages/admin/layout";
import ChannelsPage from "@/pages/admin/ai/channels";
import ModelsPage from "@/pages/admin/ai/models";
import OverviewPage from "@/pages/admin/ai/overview";
import PluginsPage from "@/pages/admin/ai/plugins";
import ImageProcessorPage from "@/pages/admin/image-processor";
import AgentSkillsPage from "@/pages/admin/agent-skills";
import StoragePage from "@/pages/admin/storage";
import UsersPage from "@/pages/admin/users";
import EmailSettingsPage from "@/pages/admin/settings/email-settings";
import RegisterSettingsPage from "@/pages/admin/settings/register-settings";
import Canvas from "@/pages/canvas";
import CanvasList from "@/pages/canvas-list";
import Home from "@/pages/home";
import HomeLayout from "@/pages/home/layout";
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

/** 根布局：全站弹窗挂在路由里面，弹窗内可以用 Link / useNavigate */
function RootLayout() {
  return (
    <>
      <Outlet />
      <DialogHost />
    </>
  );
}

export const router = createBrowserRouter([
  {
    element: <RootLayout />,
    children: [
      {
        path: "/login",
        element: <Login />,
      },
      {
        element: <RequireAuth />,
        children: [
          {
            /** 首页和所有画布共用侧栏 + 顶栏 */
            element: <HomeLayout />,
            children: [
              { index: true, element: <Home /> },
              { path: "canvases", element: <CanvasList /> },
            ],
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
            path: "admin",
            element: <AdminLayout />,
            children: [
              { index: true, element: <Navigate to="ai/overview" replace /> },
              { path: "ai", element: <Navigate to="overview" replace /> },
              { path: "ai/overview", element: <OverviewPage /> },
              { path: "ai/models", element: <ModelsPage /> },
              { path: "ai/models/new", element: <ModelsPage /> },
              { path: "ai/channels", element: <ChannelsPage /> },
              { path: "ai/plugins", element: <PluginsPage /> },
              { path: "agent", element: <Navigate to="skills" replace /> },
              { path: "agent/skills", element: <AgentSkillsPage /> },
              { path: "users", element: <UsersPage /> },
              { path: "settings", element: <Navigate to="storage" replace /> },
              { path: "settings/storage", element: <StoragePage /> },
              { path: "settings/image-processor", element: <ImageProcessorPage /> },
              { path: "settings/register", element: <RegisterSettingsPage /> },
              { path: "settings/email", element: <EmailSettingsPage /> },
            ],
          },
        ],
      },
    ],
  },
]);
