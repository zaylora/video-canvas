import { ChevronRight } from "lucide-react";
import { createContext, useContext, useId, useState, type ComponentProps } from "react";

import { cn } from "@/lib/utils";

type DisclosureContextValue = { open: boolean; toggle: () => void; contentId: string };

const DisclosureContext = createContext<DisclosureContextValue | null>(null);

function useDisclosure(): DisclosureContextValue {
  const ctx = useContext(DisclosureContext);
  if (!ctx) throw new Error("DisclosureTrigger / DisclosureContent 必须放在 Disclosure 里");
  return ctx;
}

/**
 * 折叠区（工具参数、思考、模型调用这类默认收起的内容）：
 * <Disclosure><DisclosureTrigger>标题行</DisclosureTrigger><DisclosureContent>内容</DisclosureContent></Disclosure>
 * 内容只在展开时渲染，所以折叠的大段文本不占 DOM；触发行是带 aria-expanded 的按钮，键盘可操作。
 * @param defaultOpen 初始是否展开，默认折叠
 */
function Disclosure({
  defaultOpen = false,
  className,
  ...props
}: ComponentProps<"div"> & { defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  const contentId = useId();
  return (
    <DisclosureContext.Provider value={{ open, toggle: () => setOpen((v) => !v), contentId }}>
      <div
        data-slot="disclosure"
        data-state={open ? "open" : "closed"}
        className={className}
        {...props}
      />
    </DisclosureContext.Provider>
  );
}

/** 触发行：整行可点，左侧的箭头展开时转 90 度（只对 transform 做动画） */
function DisclosureTrigger({ className, children, ...props }: ComponentProps<"button">) {
  const { open, toggle, contentId } = useDisclosure();
  return (
    <button
      type="button"
      data-slot="disclosure-trigger"
      aria-expanded={open}
      aria-controls={contentId}
      className={cn(
        "focus-visible:ring-ring/50 flex w-full min-w-0 items-center gap-1.5 rounded-md text-left outline-none focus-visible:ring-2",
        className,
      )}
      onClick={toggle}
      {...props}
    >
      <ChevronRight
        aria-hidden="true"
        className={cn(
          "text-muted-foreground size-3.5 shrink-0 transition-transform motion-reduce:transition-none",
          open && "rotate-90",
        )}
      />
      {children}
    </button>
  );
}

/** 折叠的内容；只在展开时渲染 */
function DisclosureContent({ className, ...props }: ComponentProps<"div">) {
  const { open, contentId } = useDisclosure();
  if (!open) return null;
  return <div data-slot="disclosure-content" id={contentId} className={className} {...props} />;
}

export { Disclosure, DisclosureContent, DisclosureTrigger };
