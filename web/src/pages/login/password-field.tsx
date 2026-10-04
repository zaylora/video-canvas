import { useState, type ComponentProps } from "react";
import { Eye, EyeOff } from "lucide-react";

import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

/** 带显示 / 隐藏切换的密码输入框，登录与注册共用 */
export function PasswordField({ className, ...props }: Omit<ComponentProps<typeof Input>, "type">) {
  const [shown, setShown] = useState(false);
  return (
    <div className="relative">
      <Input
        type={shown ? "text" : "password"}
        className={cn("h-11 pr-11", className)}
        {...props}
      />
      <button
        type="button"
        aria-label={shown ? "隐藏密码" : "显示密码"}
        aria-pressed={shown}
        onClick={() => setShown((value) => !value)}
        className="text-muted-foreground hover:text-foreground focus-visible:outline-ring absolute top-1/2 right-3 -translate-y-1/2 rounded p-1 focus-visible:outline-2 focus-visible:outline-offset-2"
      >
        {shown ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
      </button>
    </div>
  );
}
