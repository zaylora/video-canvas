import { useEffect } from "react";
import { createBrowserRouter, Navigate, Outlet, useLocation } from "react-router";

import { DialogHost } from "@/components/dialog-host";
import { WsRuntime } from "@/components/ws-runtime";
import AdminLayout from "@/pages/admin/layout";
import ChannelsPage from "@/pages/admin/ai/channels";
import { channelsPagePath, legacyModelsRedirect } from "@/utils/admin/channel-route";
import PluginsPage from "@/pages/admin/ai/plugins";
import ImageProcessorPage from "@/pages/admin/image-processor";
import AgentSkillsPage from "@/pages/admin/agent-skills";
import AgentTokensPage from "@/pages/admin/agent-tokens";
import StoragePage from "@/pages/admin/storage";
import UsersPage from "@/pages/admin/users";
import PaymentAdminPage from "@/pages/admin/payment";
import UsagePage from "@/pages/admin/usage";
import AuditsPage from "@/pages/admin/logs";
import EmailSettingsPage from "@/pages/admin/settings/email-settings";
import ShowcaseSettingsPage from "@/pages/admin/settings/showcase";
import RegisterSettingsPage from "@/pages/admin/settings/register-settings";
import VersionSettingsPage from "@/pages/admin/settings/version-settings";
import Assets from "@/pages/assets";
import Canvas from "@/pages/canvas";
import CanvasList from "@/pages/home/canvas-list";
import Conversation from "@/pages/conversation";
import Home from "@/pages/home";
import HomeLayout from "@/pages/home/layout";
import Login from "@/pages/login";
import PayReturn from "@/pages/pay/return";
import Profile from "@/pages/profile";
import { useMeStore } from "@/store/me";
import { getToken } from "@/utils/storage/token";

/**
 * 旧的模型页地址：模型管理已并入渠道与模型主从页，保留 ?channel= / ?model= / ?test= 查询串，
 * 旧代码经路由 state 带来的模型也转成查询参数。
 */
function LegacyModelsRedirect() {
  const location = useLocation();
  return <Navigate to={legacyModelsRedirect(location.search, location.state)} replace />;
}

function RequireAuth() {
  const location = useLocation();
  const loggedIn = !!getToken();

  /** 登录后进入应用、或带着有效令牌启动时，拉一次当前用户资料（侧栏、画布右上角、个人中心共用） */
  useEffect(() => {
    if (loggedIn) void useMeStore.getState().fetchMe();
  }, [loggedIn]);

  if (!loggedIn) {
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
      /** 付款完成后渠道把浏览器跳回这里：不要求登录态已就绪，页面自己处理未登录 */
      {
        path: "/pay/return",
        element: <PayReturn />,
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
              { path: "assets", element: <Assets /> },
              /** 旧的「新对话」地址：新建对话现在就是创作页 */
              { path: "conversations/new", element: <Navigate to="/" replace /> },
              { path: "conversations/:id", element: <Conversation /> },
              { path: "profile", element: <Profile /> },
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
              {
                path: "ai/overview",
                /** 总览带图表库（recharts，体量大），按路由拆成单独的包，不拖慢画布与其他页面的首屏 */
                lazy: async () => ({
                  Component: (await import("@/pages/admin/ai/overview")).default,
                }),
              },
              {
                path: "ai/queue",
                /** 任务队列页按路由拆包：只有运维 / 管理员才会打开 */
                lazy: async () => ({
                  Component: (await import("@/pages/admin/ai/queue")).default,
                }),
              },
              { path: "ai/models", element: <LegacyModelsRedirect /> },
              // 旧链接：新建模型曾占用这个地址，现在模型在渠道与模型主从页里管理
              { path: "ai/models/new", element: <Navigate to={channelsPagePath()} replace /> },
              { path: "ai/channels", element: <ChannelsPage /> },
              { path: "ai/plugins", element: <PluginsPage /> },
              { path: "agent", element: <Navigate to="skills" replace /> },
              { path: "agent/skills", element: <AgentSkillsPage /> },
              { path: "agent/tokens", element: <AgentTokensPage /> },
              { path: "users", element: <UsersPage /> },
              { path: "payment", element: <PaymentAdminPage /> },
              { path: "usage", element: <UsagePage /> },
              { path: "logs", element: <Navigate to="audits" replace /> },
              { path: "logs/audits", element: <AuditsPage /> },
              { path: "settings", element: <Navigate to="storage" replace /> },
              { path: "settings/storage", element: <StoragePage /> },
              { path: "settings/image-processor", element: <ImageProcessorPage /> },
              { path: "settings/register", element: <RegisterSettingsPage /> },
              { path: "settings/email", element: <EmailSettingsPage /> },
              { path: "settings/showcase", element: <ShowcaseSettingsPage /> },
              { path: "settings/version", element: <VersionSettingsPage /> },
            ],
          },
        ],
      },
    ],
  },
]);
