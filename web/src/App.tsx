import { RouterProvider } from "react-router";

import { Toaster } from "@/components/ui/sonner";
import { router } from "@/router";
import { initTheme } from "@/store";

// 挂载前先把存档里的主题贴上去，否则深色会先闪一下浅色
initTheme();

function App() {
  return (
    <>
      <RouterProvider router={router} />
      <Toaster />
    </>
  );
}

export default App;
