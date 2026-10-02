import { useMemo } from "react";
import { useNodes } from "@xyflow/react";
import { FolderOpen } from "lucide-react";

import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import type { CanvasNode } from "@/types";
import { readOutputs } from "@/utils/canvas/outputs";

import { useFocusNode } from "./use-focus-node";

type AssetItem = {
  key: string;
  nodeId: string;
  nodeLabel: string;
  src: string;
  mediaType: "image" | "video" | "audio";
  active: boolean;
};

/** 画布素材：所有节点的历次产物和上传素材，点一张定位到它所在的节点 */
function AssetGrid({ onPicked }: { onPicked: () => void }) {
  const nodes = useNodes<CanvasNode>();
  const focusNode = useFocusNode();
  const items = useMemo<AssetItem[]>(
    () =>
      nodes.flatMap((node) => {
        const outputs = readOutputs(node.data);
        if (outputs.length === 0 && node.data.src && node.data.mediaType) {
          return [
            {
              key: `${node.id}:src`,
              nodeId: node.id,
              nodeLabel: node.data.label,
              src: node.data.src,
              mediaType: node.data.mediaType,
              active: true,
            },
          ];
        }
        return outputs.map((output) => ({
          key: `${node.id}:${output.id}`,
          nodeId: node.id,
          nodeLabel: node.data.label,
          src: output.src,
          mediaType: output.mediaType,
          active: output.src === node.data.src,
        }));
      }),
    [nodes],
  );

  if (items.length === 0) {
    return (
      <div className="text-muted-foreground flex flex-1 flex-col items-center justify-center gap-3 p-8 text-center text-sm">
        <FolderOpen className="size-8 opacity-40" />
        画布里还没有素材。生成结果和上传的文件都会出现在这里。
      </div>
    );
  }

  return (
    <div className="grid grid-cols-2 gap-3 overflow-y-auto p-4 pt-0">
      {items.map((item) => (
        <button
          key={item.key}
          type="button"
          className="group/asset focus-visible:ring-node-ring/60 flex flex-col gap-1.5 rounded-xl text-left outline-none focus-visible:ring-2"
          onClick={() => {
            if (focusNode(item.nodeId)) onPicked();
          }}
        >
          <span className="bg-muted ring-border relative aspect-video overflow-hidden rounded-xl ring-1 transition-shadow group-hover/asset:ring-foreground/30">
            {item.mediaType === "image" ? (
              <img
                src={item.src}
                alt=""
                loading="lazy"
                decoding="async"
                className="size-full object-cover"
              />
            ) : item.mediaType === "video" ? (
              <video src={item.src} muted preload="metadata" className="size-full object-cover" />
            ) : (
              <span className="text-muted-foreground grid size-full place-items-center text-xs">
                音频
              </span>
            )}
            {item.active && (
              <span className="bg-background/80 absolute top-1.5 left-1.5 rounded-md px-1.5 py-0.5 text-[10px] backdrop-blur">
                当前版本
              </span>
            )}
          </span>
          <span className="text-muted-foreground truncate text-xs">{item.nodeLabel}</span>
        </button>
      ))}
    </div>
  );
}

export function AssetsSheet({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="left" className="w-96 gap-0 sm:max-w-96">
        <SheetHeader>
          <SheetTitle>画布素材</SheetTitle>
          <SheetDescription>点一张素材，定位到它所在的节点</SheetDescription>
        </SheetHeader>
        {open && <AssetGrid onPicked={() => onOpenChange(false)} />}
      </SheetContent>
    </Sheet>
  );
}
