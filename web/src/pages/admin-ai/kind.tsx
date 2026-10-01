import { AudioLines, Clapperboard, Image as ImageIcon, Type, type LucideIcon } from "lucide-react";

import { Tag, toneClasses, type TagTone } from "@/components/admin-ui/tag";
import { cn } from "@/lib/utils";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";

/** 能力的展示顺序（设计稿：文本、图片、视频、音频） */
export const KIND_ORDER = ["text", "image", "video", "audio"] as const;

/** 能力的图标与颜色（设计稿：文本蓝、图片紫、视频红、音频橙） */
export const KIND_STYLE: Record<string, { icon: LucideIcon; tone: TagTone }> = {
  text: { icon: Type, tone: "info" },
  image: { icon: ImageIcon, tone: "violet" },
  video: { icon: Clapperboard, tone: "rose" },
  audio: { icon: AudioLines, tone: "orange" },
};

/** 能力标签：图标 + 文字 */
export function KindTag({ kind }: { kind: string }) {
  const style = KIND_STYLE[kind];
  if (!style) return <Tag>{kind || "—"}</Tag>;
  return (
    <Tag tone={style.tone}>
      <style.icon />
      {MODEL_KIND_LABEL[kind] ?? kind}
    </Tag>
  );
}

/** 四种能力的小图标排成一行：支持的上色，不支持的淡出（插件列表、版本历史用） */
export function KindIcons({ kinds }: { kinds: string[] }) {
  return (
    <span className="inline-flex items-center gap-1">
      {KIND_ORDER.map((kind) => {
        const style = KIND_STYLE[kind];
        const on = kinds.includes(kind);
        return (
          <span
            key={kind}
            title={`${MODEL_KIND_LABEL[kind]}${on ? "" : "（不支持）"}`}
            className={cn(
              "grid size-5 place-items-center rounded",
              on ? cn("border", toneClasses[style.tone]) : "text-muted-foreground/30",
            )}
          >
            <style.icon className="size-3" />
          </span>
        );
      })}
    </span>
  );
}
