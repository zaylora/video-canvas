import { useRef } from "react";
import { useStore } from "@xyflow/react";
import { ArrowRight, LayoutGrid, TriangleAlert } from "lucide-react";
import { toast } from "sonner";

import { ChromeButton, ChromePill, ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { cn } from "@/lib/utils";
import type { CanvasNode } from "@/types";

import { useFocusNode } from "./use-focus-node";

/** 右下角：连线数、节点数、失败节点数（点一下依次定位到失败的节点） */
export function StatsBar() {
  const edgeCount = useStore((state) => state.edges.length);
  // 组不算素材节点
  const nodeCount = useStore((state) => state.nodes.filter((node) => node.type !== "group").length);
  const failedKey = useStore((state) =>
    (state.nodes as CanvasNode[])
      .filter((node) => node.type === "canvas" && node.data.status === "error")
      .map((node) => node.id)
      .join(","),
  );
  const failed = failedKey ? failedKey.split(",") : [];
  const focusNode = useFocusNode();
  const cursor = useRef(0);

  return (
    <ChromePill className="font-mono text-xs">
      <span className="text-muted-foreground flex h-8 items-center gap-1 px-2" title="连线">
        <ArrowRight className="size-3.5" />
        <span className="tabular-nums">{edgeCount}</span>
      </span>
      <span className="text-muted-foreground flex h-8 items-center gap-1 px-2" title="节点">
        <LayoutGrid className="size-3.5" />
        <span className="tabular-nums">{nodeCount}</span>
      </span>
      <ChromeTooltip label={failed.length ? "定位失败的节点" : "没有失败的节点"}>
        <ChromeButton
          aria-label={`失败节点 ${failed.length} 个`}
          className={cn("gap-1 font-mono text-xs", failed.length && "text-status-warning")}
          onClick={() => {
            if (!failed.length) {
              toast.info("没有失败的节点");
              return;
            }
            const id = failed[cursor.current % failed.length];
            cursor.current += 1;
            focusNode(id);
          }}
        >
          <TriangleAlert className="size-3.5!" />
          <span className="tabular-nums">{failed.length}</span>
        </ChromeButton>
      </ChromeTooltip>
    </ChromePill>
  );
}
