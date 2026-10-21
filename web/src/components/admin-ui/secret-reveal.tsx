import { KeyRound } from "lucide-react";
import type { ComponentProps } from "react";

import { toneClasses } from "@/components/admin-ui/tag";
import { cn } from "@/lib/utils";

/**
 * 一次性展示的密钥提示块（绿色）：令牌、密码这类关掉就再也看不到的明文。
 * <SecretReveal>
 *   <SecretRevealTitle>令牌已签发，明文只显示这一次</SecretRevealTitle>
 *   <SecretRevealValue><SecretRevealCode>{secret}</SecretRevealCode><CopyButton text={secret} /></SecretRevealValue>
 *   <SecretRevealHint>怎么保存的说明</SecretRevealHint>
 *   <SecretRevealActions><Button>我已保存</Button></SecretRevealActions>
 * </SecretReveal>
 * 用 status 角色，读屏只播报标题；明文本身不放进 aria-live 区域之外。
 */
function SecretReveal({ className, children, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="secret-reveal"
      role="status"
      className={cn(
        "flex items-start gap-2.5 rounded-lg border px-3 py-2.5 text-sm",
        toneClasses.success,
        className,
      )}
      {...props}
    >
      <KeyRound className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
      <div className="flex min-w-0 flex-1 flex-col gap-2">{children}</div>
    </div>
  );
}

function SecretRevealTitle({ className, ...props }: ComponentProps<"p">) {
  return <p data-slot="secret-reveal-title" className={cn("font-medium", className)} {...props} />;
}

/** 明文和它的复制按钮排一行，窄屏换行 */
function SecretRevealValue({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="secret-reveal-value"
      className={cn("flex flex-wrap items-center gap-2", className)}
      {...props}
    />
  );
}

function SecretRevealCode({ className, ...props }: ComponentProps<"code">) {
  return (
    <code
      data-slot="secret-reveal-code"
      className={cn(
        "bg-background/60 min-w-0 rounded-md px-2 py-1 font-mono text-xs break-all select-all",
        className,
      )}
      {...props}
    />
  );
}

function SecretRevealHint({ className, ...props }: ComponentProps<"p">) {
  return (
    <p
      data-slot="secret-reveal-hint"
      className={cn("text-xs opacity-80 [&_code]:font-mono", className)}
      {...props}
    />
  );
}

function SecretRevealActions({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="secret-reveal-actions"
      className={cn("flex items-center gap-2", className)}
      {...props}
    />
  );
}

export {
  SecretReveal,
  SecretRevealActions,
  SecretRevealCode,
  SecretRevealHint,
  SecretRevealTitle,
  SecretRevealValue,
};
