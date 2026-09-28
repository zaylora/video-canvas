import { createBrowserRouter, Navigate, Outlet, useLocation } from "react-router";

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

  return <Outlet />;
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
    ],
  },
]);
