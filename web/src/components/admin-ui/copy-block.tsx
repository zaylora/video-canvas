import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * 可复制的文本块（CORS 规则、配置片段）：标题行放说明与复制按钮，下面是等宽文本。
 * <CopyBlock><CopyBlockHeader><CopyBlockTitle>需要配置 CORS</CopyBlockTitle><CopyButton text={rules} /></CopyBlockHeader><CopyBlockCode>{rules}</CopyBlockCode></CopyBlock>
 */
function CopyBlock({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="copy-block"
      className={cn("bg-muted/40 flex flex-col gap-2 rounded-lg border p-3 text-xs", className)}
      {...props}
    />
  );
}

function CopyBlockHeader({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="copy-block-header"
      className={cn("flex items-center gap-2 font-medium", className)}
      {...props}
    />
  );
}

function CopyBlockTitle({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="copy-block-title"
      className={cn("flex min-w-0 flex-1 items-center gap-1.5 [&>svg]:size-3.5", className)}
      {...props}
    />
  );
}

function CopyBlockCode({ className, ...props }: ComponentProps<"pre">) {
  return (
    <pre
      data-slot="copy-block-code"
      className={cn("overflow-x-auto font-mono text-xs leading-relaxed", className)}
      {...props}
    />
  );
}

export { CopyBlock, CopyBlockCode, CopyBlockHeader, CopyBlockTitle };
