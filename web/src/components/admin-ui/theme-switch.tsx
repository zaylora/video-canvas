import { Moon, Sun } from "lucide-react";

import { Button } from "@/components/ui/button";

/** 顶栏的主题切换（设计稿样式）：圆形按钮，点一下在浅色 / 深色之间切换 */
function ThemeSwitch({ dark, onToggle }: { dark: boolean; onToggle: () => void }) {
  return (
    <Button
      variant="ghost"
      size="icon-sm"
      className="rounded-full"
      aria-label={dark ? "切换到浅色" : "切换到深色"}
      onClick={onToggle}
    >
      {dark ? <Moon /> : <Sun />}
    </Button>
  );
}

export { ThemeSwitch };
