import { useState } from "react";
import { motion } from "motion/react";
import { Loader2, Lock, Plus, ScrollText, Trash2, Users } from "lucide-react";
import { toast } from "sonner";

import { addEpisode, deleteEpisode } from "@/api/drama";
import { dramaErrorMessage } from "@/api/drama/errors";
import type { EpisodeSummary } from "@/api/drama/type";
import { ActionContextMenu } from "@/components/action-context-menu";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useDramaSegmentsStore } from "@/store/drama-segments";
import { deleteEpisodeConfirm, episodeAfterDelete } from "@/utils/drama/episodes";
import { busyReason } from "@/utils/drama/jobs";
import {
  episodePips,
  type PipState,
  type ScriptDoc,
  type WorkspaceStage,
} from "@/utils/drama/stages";

import { useWorkspace } from "./context";

const PIP_STYLE: Record<PipState, string> = {
  off: "bg-muted-foreground/25",
  run: "bg-primary animate-pulse motion-reduce:animate-none",
  part: "bg-primary",
  on: "bg-status-success",
};

/** 一集下面的四格小进度条：剧本 / 拆解 / 分镜 / 成片 */
function Pips({ states }: { states: readonly PipState[] }) {
  return (
    <span aria-hidden className="flex gap-[3px]">
      {states.map((state, index) => (
        <i key={index} className={cn("h-[3px] w-1.5 rounded-full", PIP_STYLE[state])} />
      ))}
    </span>
  );
}

/**
 * 左侧分集栏：每集一个编号 + 四格进度，锁定集带小锁；右键一集可删除（后面的集号往前补位）。
 * 「当前是哪一集」在所有工序间共用（总览不放这条栏）；各工序栏里的条目都一样、位置不动，切工序时不跳：
 * 顶上是「大纲」「人物」（研读先出大纲，再按集拆分，所以排在第 1 集前面；在别的工序点它们会跳到剧本页），
 * 接着是「全集」（只有资产区按集筛选，别处置灰禁用），再往下是各集。
 */
export function EpisodeRail({
  stage,
  ep,
  doc = null,
  onPick,
  onDoc,
}: {
  stage: WorkspaceStage;
  ep: number;
  /** 剧本页打开的项目文档；打开时分集都不高亮 */
  doc?: ScriptDoc | null;
  onPick: (ep: number) => void;
  onDoc?: (doc: ScriptDoc) => void;
}) {
  const { drama, jobs, refresh } = useWorkspace();
  const [adding, setAdding] = useState(false);
  /** 有任务在跑时不能删集：任务按集号写回，删集补位会让它写错集 */
  const blocked = busyReason(jobs);

  /** 右键「删除」：确认后删，后面的集号往前补位；正在看的集跟着落到合适的位置 */
  const removeOne = async (episode: EpisodeSummary) => {
    const total = drama.episodes.length;
    const ok = await confirm({
      ...deleteEpisodeConfirm(episode, total),
      confirmLabel: "删除",
      destructive: true,
      blockReason: blocked,
      onConfirm: async () => {
        try {
          await deleteEpisode(drama.id, episode.no);
        } catch (e) {
          throw new Error(dramaErrorMessage(e, "删除失败，请稍后重试"));
        }
      },
    });
    if (!ok) return;
    useDramaSegmentsStore.getState().forgetDrama(drama.id);
    await refresh();
    toast.success(`已删除第 ${episode.no} 集`);
    // 资产区的「全集」、剧本页的大纲 / 人物不跟集号走，不用跳
    if (ep === 0 || doc !== null) return;
    const next = episodeAfterDelete(ep, episode.no, total - 1);
    if (next !== ep || ep === episode.no) onPick(next);
  };
  const addOne = async () => {
    setAdding(true);
    try {
      const created = await addEpisode(drama.id, { title: "新的一集" });
      toast.success(`已新增第 ${created.no} 集，可以直接手写剧本`);
      // 分集栏读自详情：先拉一次，新集出现后再切过去，否则集号会被当成无效回落
      await refresh();
      onPick(created.no);
    } catch (e) {
      toast.error(dramaErrorMessage(e, "新增失败，请稍后重试"));
    } finally {
      setAdding(false);
    }
  };
  const item =
    "focus-visible:ring-ring/50 relative flex w-full flex-col items-center gap-1.5 rounded-xl py-2 outline-none transition-colors focus-visible:ring-2";
  return (
    <nav
      aria-label="分集"
      className="border-border flex w-[78px] shrink-0 flex-col gap-1 overflow-y-auto border-r px-2 py-2.5 max-sm:hidden"
    >
      <div className="text-muted-foreground py-1 text-center font-mono text-[10.5px] tracking-[0.12em]">
        分集
      </div>
      {onDoc && (
        <>
          {PROJECT_DOCS.map(({ key, label, title, icon: Icon }) => (
            <RailItem
              key={key}
              active={stage === "script" && doc === key}
              className={item}
              label={title}
              onClick={() => onDoc(key)}
            >
              <Icon className="size-4" aria-hidden />
              <span className="text-[11px] leading-none">{label}</span>
            </RailItem>
          ))}
          <div aria-hidden className="bg-border mx-2 my-1 h-px" />
        </>
      )}
      <RailItem
        active={stage === "assets" && ep === 0}
        className={item}
        label="全集"
        disabledReason={stage === "assets" ? undefined : "仅资产管理可按全集筛选"}
        onClick={() => onPick(0)}
      >
        <span className="text-[13px] font-semibold">全集</span>
        <Pips states={["on"]} />
      </RailItem>
      {drama.episodes.map((episode) => (
        <ActionContextMenu
          key={episode.no}
          actions={[
            {
              key: "delete",
              label: "删除",
              icon: <Trash2 />,
              destructive: true,
              disabledReason: blocked,
              onSelect: () => void removeOne(episode),
            },
          ]}
        >
          <RailItem
            active={!doc && ep === episode.no}
            className={item}
            label={`第 ${episode.no} 集 · ${episode.title}`}
            onClick={() => onPick(episode.no)}
            corner={
              episode.locked && (
                <Lock
                  aria-hidden
                  className="text-muted-foreground absolute top-1.5 right-1.5 size-2.5"
                />
              )
            }
          >
            <span className="font-mono text-[15px] leading-none font-semibold tracking-tight">
              {String(episode.no).padStart(2, "0")}
            </span>
            <Pips states={episodePips(episode)} />
          </RailItem>
        </ActionContextMenu>
      ))}
      {stage === "script" && drama.episodes.length > 0 && (
        <button
          type="button"
          aria-label="新增一集"
          title="新增一集"
          disabled={adding}
          onClick={() => void addOne()}
          className="text-muted-foreground hover:bg-muted/60 hover:text-foreground focus-visible:ring-ring/50 flex w-full flex-col items-center gap-1 rounded-xl py-2 text-[11px] outline-none focus-visible:ring-2 disabled:opacity-50"
        >
          {adding ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
          新增一集
        </button>
      )}
    </nav>
  );
}

const PROJECT_DOCS = [
  { key: "outline", label: "大纲", title: "故事大纲", icon: ScrollText },
  { key: "characters", label: "人物", title: "人物传记", icon: Users },
] as const;

function RailItem({
  active,
  className,
  label,
  onClick,
  disabledReason,
  corner,
  children,
}: {
  active: boolean;
  className: string;
  label: string;
  onClick: () => void;
  /** 不能点时的原因：有就置灰禁用，悬停显示原因 */
  disabledReason?: string;
  /** 挂在按钮角上的标记（如锁定集的小锁）：相对整个按钮定位，不进内容层，免得压住集号 */
  corner?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-current={active}
      aria-label={label}
      title={disabledReason ?? label}
      disabled={!!disabledReason}
      onClick={onClick}
      className={cn(
        className,
        active
          ? "text-foreground"
          : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
        "disabled:text-muted-foreground disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent",
      )}
    >
      {active && (
        <>
          <motion.span
            layoutId="drama-rail-active"
            transition={SPRING}
            className="bg-muted absolute inset-0 rounded-xl"
          />
          <motion.span
            layoutId="drama-rail-bar"
            transition={SPRING}
            className="bg-primary absolute top-2.5 bottom-2.5 -left-2 w-[3px] rounded-r-full"
          />
        </>
      )}
      <span className="relative flex flex-col items-center gap-1.5">{children}</span>
      {corner}
    </button>
  );
}
