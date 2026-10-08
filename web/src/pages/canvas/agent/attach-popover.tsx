import { useRef, useState } from "react";
import { FileText, Image as ImageIcon, Plus } from "lucide-react";
import { toast } from "sonner";

import { ChromeTooltip } from "@/components/canvas/chrome/chrome";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";

import { AGENT_ICON_BTN, AGENT_POP } from "./styles";

/** 故事文件最大字节数：消息最多 20000 字，文件再大也放不进去 */
const MAX_STORY_BYTES = 200_000;
/** 一条消息最多多少字，与后端一致 */
const MAX_MESSAGE_CHARS = 20000;

/**
 * ＋ 附件：
 * - 上传故事或剧本（.txt / .md）：读成文字插进输入框，Agent 直接就能读到
 * - 上传图片：先在画布中心放成图片节点（走现有的上传），再把节点作为 chip 引用；Agent 用「查看图片」看它
 */
export function AttachPopover({
  onInsertText,
  onAttachImages,
  disabled,
}: {
  /** 插入一段文字 */
  onInsertText: (text: string) => void;
  /** 把图片上传到画布；调用方负责把新建的节点作为 chip 插进输入框 */
  onAttachImages: (files: File[]) => void | Promise<unknown>;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const story = useRef<HTMLInputElement>(null);
  const image = useRef<HTMLInputElement>(null);

  const readStory = async (file: File) => {
    if (file.size > MAX_STORY_BYTES) {
      toast.error("文件太大了，请控制在 20 万字节以内，或分段粘贴");
      return;
    }
    const text = (await file.text()).replace(/\r\n/g, "\n").trim();
    if (!text) {
      toast.error("这个文件是空的");
      return;
    }
    const clipped = [...text].slice(0, MAX_MESSAGE_CHARS);
    if (clipped.length < [...text].length)
      toast.info(`故事太长，只放进了前 ${MAX_MESSAGE_CHARS} 字`);
    onInsertText(`${file.name.replace(/\.[^.]+$/, "")}：\n${clipped.join("")}`);
  };

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
          "w-[min(230px,calc(100vw-24px))] origin-bottom-left gap-0.5 p-1.5",
        )}
      >
        <button
          type="button"
          onClick={() => image.current?.click()}
          className="hover:bg-chrome-hover focus-visible:ring-node-ring/60 flex items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[13px] outline-none focus-visible:ring-2"
        >
          <ImageIcon className="size-4" /> 上传图片
        </button>
        <button
          type="button"
          onClick={() => story.current?.click()}
          className="hover:bg-chrome-hover focus-visible:ring-node-ring/60 flex items-center gap-2 rounded-lg px-2.5 py-2 text-left text-[13px] outline-none focus-visible:ring-2"
        >
          <FileText className="size-4" /> 上传故事或剧本
        </button>
        <input
          ref={story}
          type="file"
          accept=".txt,.md,text/plain,text/markdown"
          className="sr-only"
          tabIndex={-1}
          aria-hidden
          onChange={(e) => {
            const file = e.target.files?.[0];
            e.target.value = "";
            setOpen(false);
            if (file) void readStory(file);
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
            const files = Array.from(e.target.files ?? []);
            e.target.value = "";
            setOpen(false);
            if (files.length) void onAttachImages(files);
          }}
        />
      </PopoverContent>
    </Popover>
  );
}
