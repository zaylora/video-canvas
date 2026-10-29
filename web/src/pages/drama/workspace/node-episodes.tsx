import { CalendarRange } from "lucide-react";
import { useReactFlow } from "@xyflow/react";

import { ChromeButton } from "@/components/canvas/chrome/chrome";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import type { CanvasEdge, CanvasNode, CanvasNodeData } from "@/types";
import { assetEpisodesOf, episodesLabel, setEpisodes, toggleEpisode } from "@/utils/drama/assets";
import { cn } from "@/lib/utils";

import { useWorkspace } from "./context";

/** 类型选项：由节点种类决定能改成什么（角色卡节点只能是角色，图片节点可以是场景 / 道具） */
function typeChoices(data: CanvasNodeData): { key: string; label: string }[] {
  if (data.kind === "character") return [{ key: "character", label: "角色" }];
  if (data.kind === "image") {
    return [
      { key: "", label: "图片" },
      { key: "scene", label: "场景" },
      { key: "prop", label: "道具" },
    ];
  }
  return [{ key: "", label: "已分类" }];
}

/**
 * 节点功能区的「出现集数」（只在短剧资产画布出现，由 toolbarExtra 注入）：
 * 读写节点的 data.drama —— 类型（角色 / 场景 / 道具 / 未分类）和出现集数（都不勾 = 全集）。
 * 空节点（无素材无历史）默认没有功能区，短剧画布经 toolbarVisible 放行，所以这里空节点也能用。
 */
export function NodeEpisodesButton({ id, data }: { id: string; data: CanvasNodeData }) {
  const { updateNodeData } = useReactFlow<CanvasNode, CanvasEdge>();
  const { drama } = useWorkspace();
  const episodes = assetEpisodesOf(data);
  const classified = episodes !== null;
  const choices = typeChoices(data);

  return (
    <Popover>
      <PopoverTrigger
        render={
          <ChromeButton
            aria-label="出现集数"
            title="出现集数"
            className="text-foreground gap-1.5 px-2.5"
          />
        }
      >
        <CalendarRange />
        <span className="max-md:hidden">出现集数 · {episodesLabel(episodes)}</span>
      </PopoverTrigger>
      <PopoverContent
        side="top"
        align="start"
        sideOffset={10}
        className="nodrag nowheel w-72 gap-2 rounded-xl p-3"
      >
        <div className="text-muted-foreground text-xs">资产类型</div>
        <div className="flex flex-wrap gap-1.5">
          {choices.map((choice) => {
            const on = classified && (data.drama?.type ?? "") === choice.key;
            return (
              <button
                key={choice.key}
                type="button"
                aria-pressed={on}
                onClick={() =>
                  updateNodeData(id, {
                    drama: {
                      ...(data.drama ?? {}),
                      type: (choice.key || undefined) as "scene" | "prop" | "character" | undefined,
                      episodes: episodes ?? [],
                    },
                  })
                }
                className={cn(
                  "focus-visible:ring-ring/50 h-7 rounded-md border px-2.5 text-xs outline-none focus-visible:ring-2",
                  on ? "bg-primary text-primary-foreground border-transparent" : "hover:bg-muted",
                )}
              >
                {choice.label}
              </button>
            );
          })}
          <button
            type="button"
            aria-pressed={!classified}
            onClick={() => updateNodeData(id, { drama: undefined })}
            className={cn(
              "focus-visible:ring-ring/50 h-7 rounded-md border px-2.5 text-xs outline-none focus-visible:ring-2",
              !classified
                ? "bg-primary text-primary-foreground border-transparent"
                : "hover:bg-muted",
            )}
          >
            未分类
          </button>
        </div>

        <div className="text-muted-foreground pt-1 text-xs">出现集数 · 都不勾 = 全集</div>
        <div className="grid grid-cols-6 gap-1">
          {drama.episodes.map((ep) => {
            const on = !!episodes?.includes(ep.no);
            return (
              <button
                key={ep.no}
                type="button"
                aria-pressed={on}
                disabled={!classified}
                title={classified ? ep.title : "先选资产类型"}
                onClick={() =>
                  updateNodeData(id, setEpisodes(data, toggleEpisode(episodes ?? [], ep.no)))
                }
                className={cn(
                  "focus-visible:ring-ring/50 h-7 rounded-md border text-xs tabular-nums outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-40",
                  on ? "bg-primary text-primary-foreground border-transparent" : "hover:bg-muted",
                )}
              >
                {ep.no}
              </button>
            );
          })}
        </div>
        {drama.episodes.length === 0 && (
          <div className="text-muted-foreground text-xs">还没有分集，研读完成后可以选择。</div>
        )}
      </PopoverContent>
    </Popover>
  );
}
