import { useRef, useState } from "react";
import { FileText, Image as ImageIcon, Plus } from "lucide-react";
import { toast } from "sonner";

import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { MAX_ATTACH_IMAGES, pickImageFiles } from "@/utils/agent/image-files";

import { AGENT_ICON_BTN, AGENT_POP } from "./styles";

/**
 * ＋ 附件：
 * - 上传故事或剧本（.txt / .md）：上传成素材，作为文件卡片附件跟着消息发给 Agent，不往输入框里塞字
 * - 上传图片：先在画布中心放成图片节点（走现有的上传），再把节点作为 chip 引用；Agent 用「查看图片」看它
 */
export function AttachPopover({
  onAttachDocuments,
  onAttachImages,
  disabled,
}: {
  /** 把选好的文档（.txt / .md）加进附件托盘；类型、大小、数量的校验由托盘负责 */
  onAttachDocuments: (files: File[]) => void;
  /** 把图片上传到画布；调用方负责把新建的节点作为 chip 插进输入框 */
  onAttachImages: (files: File[]) => void | Promise<unknown>;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const story = useRef<HTMLInputElement>(null);
  const image = useRef<HTMLInputElement>(null);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <ChromeTooltip label="添加附件" side="top">
        <PopoverTrigger
          aria-label="添加附件"
          disabled={disabled}
          className={cn(AGENT_ICON_BTN, "size-8 [&_svg]:size-[17px]")}
        >
          <Plus className="size-4" />
        </PopoverTrigger>
      </ChromeTooltip>
      <PopoverContent
        side="top"
        align="start"
        sideOffset={8}
        className={cn(
          AGENT_POP,
          "w-[min(250px,calc(100vw-24px))] origin-bottom-left gap-0.5 p-1.5",
        )}
      >
        <button
          type="button"
          onClick={() => image.current?.click()}
          className="hover:bg-chrome-hover focus-visible:ring-node-ring/60 flex items-start gap-2 rounded-lg px-2.5 py-2 text-left text-[13px] outline-none focus-visible:ring-2"
        >
          <ImageIcon className="mt-0.5 size-4 shrink-0" />
          <span className="min-w-0">
            <span className="block">上传图片</span>
            <span className="text-muted-foreground block text-xs leading-snug">
              放到画布并引用，也可以直接粘贴或拖进输入框
            </span>
          </span>
        </button>
        <button
          type="button"
          onClick={() => story.current?.click()}
          className="hover:bg-chrome-hover focus-visible:ring-node-ring/60 flex items-start gap-2 rounded-lg px-2.5 py-2 text-left text-[13px] outline-none focus-visible:ring-2"
        >
          <FileText className="mt-0.5 size-4 shrink-0" />
          <span className="min-w-0">
            <span className="block">上传故事或剧本</span>
            <span className="text-muted-foreground block text-xs leading-snug">
              选一个 .txt 或 .md 文件，作为附件发给 Agent
            </span>
          </span>
        </button>
        <input
          ref={story}
          type="file"
          accept=".txt,.md,.markdown,text/plain,text/markdown"
          multiple
          className="sr-only"
          tabIndex={-1}
          aria-hidden
          onChange={(e) => {
            const picked = Array.from(e.target.files ?? []);
            e.target.value = "";
            setOpen(false);
            if (picked.length) onAttachDocuments(picked);
          }}
        />
        <input
          ref={image}
          type="file"
          accept="image/*"
          multiple
          className="sr-only"
          tabIndex={-1}
          aria-hidden
          onChange={(e) => {
            const { images, clipped } = pickImageFiles(e.target.files);
            e.target.value = "";
            setOpen(false);
            if (clipped > 0)
              toast.info(`一次最多附 ${MAX_ATTACH_IMAGES} 张图，多出的 ${clipped} 张没放进来`);
            if (images.length) void onAttachImages(images);
          }}
        />
      </PopoverContent>
    </Popover>
  );
}
