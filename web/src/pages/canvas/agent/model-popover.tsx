import { useState } from "react";
import { LayoutGroup, motion } from "motion/react";
import { Box, Plus } from "lucide-react";

import { VendorAvatar } from "@/components/admin-ui/vendor-avatar";
import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useRemoteModels } from "@/hooks/use-models";
import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { Chip } from "@/utils/agent/chips";

const KINDS = [
  { value: "image", label: "图片" },
  { value: "video", label: "视频" },
] as const;

/** 一类模型的清单；点一行插入模型 chip，不关弹层，可以连续插入多个 */
function ModelList({
  kind,
  onPick,
}: {
  kind: (typeof KINDS)[number]["value"];
  onPick: (chip: Chip) => void;
}) {
  const { options, status } = useRemoteModels(kind);
  if (status === "loading" || status === "idle")
    return <p className="text-muted-foreground px-3 py-6 text-center text-xs">加载中…</p>;
  if (options.length === 0)
    return (
      <p className="text-muted-foreground px-3 py-6 text-center text-xs">
        还没有已发布的{kind === "image" ? "图片" : "视频"}模型
      </p>
    );
  return (
    <ul className="flex max-h-64 flex-col gap-0.5 overflow-y-auto">
      {options.map((m) => (
        <li key={m.id}>
          <button
            type="button"
            onClick={() => onPick({ type: "model", id: m.id, name: m.label })}
            className="hover:bg-chrome-hover focus-visible:ring-node-ring/60 flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left outline-none focus-visible:ring-2"
          >
            <VendorAvatar
              vendor={m.vendor}
              name={m.label}
              seed={m.id}
              className="size-7 rounded-lg text-xs"
            />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[13px] font-medium">{m.label}</span>
              {m.hint && (
                <span className="text-muted-foreground block truncate text-xs">{m.hint}</span>
              )}
            </span>
            {m.priceLabel && (
              <span className="text-credit shrink-0 text-xs tabular-nums">{m.priceLabel}</span>
            )}
            <Plus className="text-muted-foreground size-4 shrink-0" />
          </button>
        </li>
      ))}
    </ul>
  );
}

/**
 * 插入模型：图片 / 视频分段（滑块用 layoutId），点一行就在提示词里插入模型 chip。
 * 一条消息里可以插多个，例如「角色用 A，场景用 B」。
 */
export function ModelPopover({
  onPick,
  disabled,
}: {
  onPick: (chip: Chip) => void;
  disabled?: boolean;
}) {
  const [kind, setKind] = useState<(typeof KINDS)[number]["value"]>("image");
  return (
    <Popover>
      <ChromeTooltip label="插入模型" side="top">
        <PopoverTrigger
          aria-label="插入模型"
          disabled={disabled}
          className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground data-popup-open:bg-chrome-hover focus-visible:ring-node-ring/60 grid size-8 place-items-center rounded-lg outline-none focus-visible:ring-2 disabled:opacity-40"
        >
          <Box className="size-4" />
        </PopoverTrigger>
      </ChromeTooltip>
      <PopoverContent
        side="top"
        align="start"
        sideOffset={8}
        className="w-[min(340px,calc(100vw-24px))] origin-bottom-left gap-2 p-2"
      >
        <LayoutGroup id="agent-model-kind">
          <div
            role="radiogroup"
            aria-label="模型种类"
            className="bg-foreground/5 flex rounded-lg p-0.5"
          >
            {KINDS.map((k) => (
              <button
                key={k.value}
                type="button"
                role="radio"
                aria-checked={kind === k.value}
                onClick={() => setKind(k.value)}
                className={cn(
                  "relative flex-1 rounded-md py-1 text-xs outline-none",
                  kind === k.value ? "text-foreground" : "text-muted-foreground",
                )}
              >
                {kind === k.value && (
                  <motion.span
                    layoutId="agent-model-kind-thumb"
                    transition={SPRING}
                    className="bg-popover ring-chrome-border absolute inset-0 rounded-md shadow-sm ring-1"
                  />
                )}
                <span className="relative">{k.label}</span>
              </button>
            ))}
          </div>
        </LayoutGroup>
        <ModelList kind={kind} onPick={onPick} />
        <p className="text-muted-foreground px-1 text-[11px]">
          点一行插入到提示词里，可以插入多个。
        </p>
      </PopoverContent>
    </Popover>
  );
}
