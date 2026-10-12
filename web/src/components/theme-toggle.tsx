import { Moon, Sun } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useSettingsStore } from "@/store/settings";

/**
 * 顶栏右上角的深浅色切换：图标是当前主题，点一下切到另一个。
 * 首页和后台共用；设置里选了“跟随系统”时，按系统当前的深浅色判断，点一下就锁定成相反的那个。
 */
export function ThemeToggle() {
  const theme = useSettingsStore((state) => state.theme);
  const updateSettings = useSettingsStore((state) => state.updateSettings);
  const dark =
    theme === "dark" ||
    (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);

  return (
    <Button
      variant="ghost"
      size="icon-sm"
      className="rounded-full"
      aria-label={dark ? "切换到浅色" : "切换到深色"}
      onClick={() => updateSettings("theme", dark ? "light" : "dark")}
    >
      {dark ? <Moon /> : <Sun />}
    </Button>
  );
}
