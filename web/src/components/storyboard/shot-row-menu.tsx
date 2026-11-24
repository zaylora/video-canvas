import { useState } from "react";
import {
  ArrowDownToLine,
  ArrowUpToLine,
  Copy,
  Ellipsis,
  FolderInput,
  Image,
  RefreshCw,
  Trash2,
  Video,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";

/** 菜单项：行高 40px（编码规范 6.1） */
const ITEM_CLASS =
  "hover:bg-chrome-hover focus-visible:bg-chrome-hover flex h-10 w-full items-center gap-2 rounded-xl px-3 text-left text-sm outline-none [&_svg]:size-4";

/**
 * 行操作菜单（设计 §3.5）：生成 / 导入素材、上方 / 下方插入、复制、删除。
 * 「导入」是把画布上已有的节点挂到这一镜 —— 分镜表是后来加的、素材不是从表里生成的时候用。
 * 删除要二次确认：菜单原位换成「删除第 N 镜？」，不弹全屏确认框。
 */
export function ShotRowMenu({
  number,
  onInsert,
  onDuplicate,
  onRemove,
  onGenerateImage,
  hasImageNode,
  onGenerateVideo,
  hasVideoNode,
  onImportVideo,
  onImportImage,
}: {
  number: number;
  onInsert: (side: "before" | "after") => void;
  onDuplicate: () => void;
  onRemove: () => void;
  /** 生成分镜图 / 重新生成：给不靠悬停的键盘用户一条路径 */
  onGenerateImage: () => void;
  /** 这一行已经关联了图片节点：菜单项叫「重新生成分镜图」 */
  hasImageNode: boolean;
  /** 生成视频 / 重新生成视频 */
  onGenerateVideo: () => void;
  /** 这一行已经建过视频节点：菜单项叫「重新生成视频」 */
  hasVideoNode: boolean;
  /** 挂上画布上已有的视频节点 */
  onImportVideo: () => void;
  /** 挂上画布上已有的图片节点当分镜图 */
  onImportImage: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const run = (fn: () => void) => () => {
    setOpen(false);
    fn();
  };

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setConfirming(false);
      }}
    >
      <PopoverTrigger
        render={
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={`第 ${number} 镜的操作`}
            className="nodrag text-muted-foreground opacity-0 group-hover/row:opacity-100 focus-visible:opacity-100 data-popup-open:opacity-100"
          />
        }
      >
        <Ellipsis />
      </PopoverTrigger>
      <PopoverContent
        side="bottom"
        align="end"
        className="bg-chrome ring-chrome-border w-48 gap-0 rounded-2xl p-1 shadow-lg ring-1 backdrop-blur-xl backdrop-saturate-150"
      >
        {confirming ? (
          <div className="flex flex-col gap-3 p-3">
            <p className="text-sm font-medium">删除第 {number} 镜？</p>
            <div className="flex justify-end gap-2">
              <Button variant="ghost" size="sm" onClick={() => setConfirming(false)}>
                取消
              </Button>
              <Button variant="destructive" size="sm" onClick={run(onRemove)}>
                删除
              </Button>
            </div>
          </div>
        ) : (
          <>
            <button type="button" className={ITEM_CLASS} onClick={run(onGenerateImage)}>
              {hasImageNode ? <RefreshCw /> : <Image />}
              {hasImageNode ? "重新生成分镜图" : "生成分镜图"}
            </button>
            <button type="button" className={ITEM_CLASS} onClick={run(onGenerateVideo)}>
              <Video />
              {hasVideoNode ? "重新生成视频" : "生成视频"}
            </button>
            <button type="button" className={ITEM_CLASS} onClick={run(onImportVideo)}>
              <FolderInput />
              {hasVideoNode ? "换用画布上的视频" : "导入画布上的视频"}
            </button>
            <button type="button" className={ITEM_CLASS} onClick={run(onImportImage)}>
              <FolderInput />
              {hasImageNode ? "换用画布上的图片" : "导入画布上的图片"}
            </button>
            <div role="separator" className="bg-chrome-border mx-2 my-1 h-px" />
            <button type="button" className={ITEM_CLASS} onClick={run(() => onInsert("before"))}>
              <ArrowUpToLine />
              在上方插入
            </button>
            <button type="button" className={ITEM_CLASS} onClick={run(() => onInsert("after"))}>
              <ArrowDownToLine />
              在下方插入
            </button>
            <button type="button" className={ITEM_CLASS} onClick={run(onDuplicate)}>
              <Copy />
              复制镜头
            </button>
            <div role="separator" className="bg-chrome-border mx-2 my-1 h-px" />
            <button
              type="button"
              className={cn(ITEM_CLASS, "text-destructive")}
              onClick={() => setConfirming(true)}
            >
              <Trash2 />
              删除镜头
            </button>
          </>
        )}
      </PopoverContent>
    </Popover>
  );
}
