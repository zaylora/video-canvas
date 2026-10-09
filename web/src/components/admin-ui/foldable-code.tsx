import { useState } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";

import { cn } from "@/lib/utils";
import { foldText } from "@/utils/admin/trace";

/** 等宽文本块，过长时折叠，点开看全文 */
function FoldableCode({
  text,
  className,
  defaultOpen = false,
  maxChars,
}: {
  text: string;
  className?: string;
  /** 默认展开全文 */
  defaultOpen?: boolean;
  /** 超过多少字符就折叠（默认 1500）；追踪面板窄，用更小的值 */
  maxChars?: number;
}) {
  const [open, setOpen] = useState(defaultOpen);
  const { folded, preview } = foldText(text, undefined, maxChars);
  return (
    <div data-slot="foldable-code" className={cn("flex flex-col gap-1", className)}>
      <pre className="bg-muted max-h-[32rem] overflow-auto rounded-md p-2 font-mono text-xs leading-5 break-all whitespace-pre-wrap">
        {open || !folded ? text : `${preview}\n…`}
      </pre>
      {folded && (
        <button
          type="button"
          className="text-primary flex items-center gap-1 self-start text-xs hover:underline"
          aria-expanded={open}
          onClick={() => setOpen((value) => !value)}
        >
          {open ? <ChevronUp className="size-3" /> : <ChevronDown className="size-3" />}
          {open ? "收起" : `展开全部（${text.length} 字符）`}
        </button>
      )}
    </div>
  );
}

export { FoldableCode };
