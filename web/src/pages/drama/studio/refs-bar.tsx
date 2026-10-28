import { AudioLines, Box, Image as ImageIcon, User } from "lucide-react";

import { StatusPill } from "@/components/drama/status-pill";
import { cn } from "@/lib/utils";
import type { RefView } from "@/utils/drama/segment-input";

import { useAssetThumb } from "./library";
import { useStudio } from "./use-studio";

const TYPE_ICON = { character: User, scene: ImageIcon, prop: Box } as const;

/** 参考素材条上的一项：缩略图 + 名字 + 状态标记（只用文字 / 声音未生效 / 失效 / 被截断） */
function Chip({ view }: { view: RefView }) {
  const { assets } = useStudio();
  const asset = assets.find((item) => item.id === view.id);
  // 资产已被删时没有缩略图，Hook 不能条件调用，所以用一个占位资产
  const thumb = useAssetThumb(
    asset ?? {
      id: view.id,
      type: "scene",
      name: view.label,
      look: "",
      label: view.label,
      episodes: [],
      kind: "image",
      src: null,
      generating: false,
    },
  );
  const Icon = TYPE_ICON[view.type ?? "scene"];
  const dead = view.state === "dead";
  return (
    <span
      className={cn(
        "bg-muted inline-flex h-[30px] items-center gap-1.5 rounded-lg pr-2 pl-[3px] text-xs",
        dead && "bg-destructive/10 text-destructive",
      )}
    >
      <span className="bg-background/60 text-muted-foreground grid size-6 shrink-0 place-items-center overflow-hidden rounded-md">
        {thumb && !dead ? (
          <img src={thumb} alt="" draggable={false} className="size-full object-cover" />
        ) : (
          <Icon className="size-3.5" />
        )}
      </span>
      <span className={cn("max-w-32 truncate", dead && "line-through")}>{view.label}</span>
      {view.voice === "used" && (
        <AudioLines className="text-muted-foreground size-3.5" aria-label="带入了参考音频" />
      )}
      {dead && (
        <StatusPill tone="destructive" className="h-4 text-[10px]">
          已失效
        </StatusPill>
      )}
      {view.state === "text" && (
        <StatusPill tone="warning" className="h-4 text-[10px]">
          只用文字
        </StatusPill>
      )}
      {view.truncated && (
        <StatusPill tone="warning" className="h-4 text-[10px]">
          部分未带入
        </StatusPill>
      )}
      {view.voice === "ignored" && (
        <StatusPill tone="warning" className="h-4 text-[10px]">
          声音未生效
        </StatusPill>
      )}
      {view.voice === "dropped" && (
        <StatusPill tone="warning" className="h-4 text-[10px]">
          声音未带入
        </StatusPill>
      )}
    </span>
  );
}

/**
 * 参考素材条：按提示词里的 @ 解析出这次会带上的参考图 / 音频，标出「只用文字」「声音未生效」，
 * 右上角是 n/上限（参考图张数，含首帧）。解析规则见 utils/drama/segment-input.ts。
 */
export function RefsBar() {
  const { plan } = useStudio();
  if (!plan) return null;
  return (
    <section aria-label="参考素材">
      <div className="mb-2 flex items-center gap-2">
        <span className="text-muted-foreground text-xs">参考素材</span>
        {plan.imageMax > 0 && (
          <StatusPill
            tone={plan.imageCount >= plan.imageMax ? "warning" : "muted"}
            className="tabular-nums"
          >
            {plan.imageCount}/{plan.imageMax}
          </StatusPill>
        )}
        {plan.keyframe && <StatusPill tone="running">含首帧</StatusPill>}
      </div>
      <div className="flex flex-wrap gap-1.5">
        {plan.refs.map((view) => (
          <Chip key={view.id} view={view} />
        ))}
        {plan.refs.length === 0 && (
          <span className="text-muted-foreground text-xs">还没有引用，输入 @ 或从左侧拖入</span>
        )}
      </div>
      {plan.notices.length > 0 && (
        <ul className="text-muted-foreground mt-2 grid gap-0.5 text-xs">
          {plan.notices.map((notice) => (
            <li key={notice}>· {notice}</li>
          ))}
        </ul>
      )}
    </section>
  );
}
