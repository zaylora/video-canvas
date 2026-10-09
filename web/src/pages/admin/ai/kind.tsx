import {
  AudioLines,
  Bot,
  Clapperboard,
  Image as ImageIcon,
  Type,
  type LucideIcon,
} from "lucide-react";

import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { Tag } from "@/components/admin-ui/tag";
import { cn } from "@/lib/utils";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";

/** 插件能声明的能力的展示顺序（设计稿：文本、图片、视频、音频）；agent 不走插件钩子，不在其中 */
export const PLUGIN_KIND_ORDER = ["text", "image", "video", "audio"] as const;

/** 模型能力的展示顺序：插件能力之外，多一个画布 Agent 用的对话大模型 */
export const KIND_ORDER = [...PLUGIN_KIND_ORDER, "agent"] as const;

/**
 * 能力的图标和颜色：和 Tag 同一档语气色（15% 以内的浅底 + 浅色主题 700 / 深色主题 400 的图标），
 * 文字保持中性，整页只有图标带颜色。soft 是图标底，text 是图标色。
 */
export const KIND_STYLE: Record<string, { icon: LucideIcon; text: string; soft: string }> = {
  text: { icon: Type, text: "text-sky-700 dark:text-sky-400", soft: "bg-sky-500/10" },
  image: {
    icon: ImageIcon,
    text: "text-violet-700 dark:text-violet-400",
    soft: "bg-violet-500/10",
  },
  video: { icon: Clapperboard, text: "text-rose-700 dark:text-rose-400", soft: "bg-rose-500/10" },
  audio: {
    icon: AudioLines,
    text: "text-orange-700 dark:text-orange-400",
    soft: "bg-orange-500/10",
  },
  agent: { icon: Bot, text: "text-emerald-700 dark:text-emerald-400", soft: "bg-emerald-500/10" },
};

/** 能力标签：灰底标签，只有图标带一点颜色 */
export function KindTag({ kind }: { kind: string }) {
  const style = KIND_STYLE[kind];
  if (!style) return <Tag>{kind || "—"}</Tag>;
  return (
    <Tag className="text-foreground">
      <style.icon className={style.text} />
      {MODEL_KIND_LABEL[kind] ?? kind}
    </Tag>
  );
}

/** 四种插件能力的小图标排成一行：支持的带浅色底，不支持的淡出（插件表、渠道表、版本历史用） */
export function KindIcons({ kinds }: { kinds: string[] }) {
  return (
    <span className="inline-flex items-center gap-1">
      {PLUGIN_KIND_ORDER.map((kind) => {
        const style = KIND_STYLE[kind];
        const on = kinds.includes(kind);
        return (
          <span
            key={kind}
            title={`${MODEL_KIND_LABEL[kind]}${on ? "" : "（不支持）"}`}
            className={cn(
              "grid size-6 place-items-center rounded-md",
              on ? cn(style.soft, style.text) : "text-muted-foreground/30",
            )}
          >
            <style.icon className="size-3.5" />
          </span>
        );
      })}
    </span>
  );
}

/** 表格工具条的能力筛选：全部 + 四种插件能力；slideId 让选中的浮块滑动，同一页多组分段要用不同的值 */
export function KindFilter({
  value,
  onChange,
  slideId,
}: {
  value: string;
  onChange: (kind: string) => void;
  slideId: string;
}) {
  return (
    <Segmented aria-label="按能力筛选">
      {["", ...PLUGIN_KIND_ORDER].map((kind) => (
        <SegmentedItem
          key={kind}
          slideId={slideId}
          active={value === kind}
          className="py-1"
          onClick={() => onChange(kind)}
        >
          {kind ? MODEL_KIND_LABEL[kind] : "全部"}
        </SegmentedItem>
      ))}
    </Segmented>
  );
}
