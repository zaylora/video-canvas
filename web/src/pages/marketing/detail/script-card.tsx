import { useState } from "react";
import {
  BookOpen,
  Check,
  CodeXml,
  Copy,
  Download,
  Loader2,
  Megaphone,
  Mic,
  MoreHorizontal,
  Music,
  Play,
  RotateCcw,
  ShieldBan,
  Undo2,
  Workflow,
} from "lucide-react";

import type { AngleOption, FlagType, Project, Round, Script } from "@/api/marketing/type";
import { EditableText } from "@/components/marketing/editable-text";
import { StatusPill } from "@/components/drama/status-pill";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import { useTasksStore } from "@/store/tasks";
import { angleName } from "@/utils/marketing/options";
import {
  FLAG_NAME,
  flaggedShots,
  flagReason,
  flagSummary,
  isScriptBusy,
  methodLabel,
  narrationsChanged,
  playableTakes,
  scriptRenderState,
  scriptStatusLabel,
} from "@/utils/marketing/script";
import { marketingNodeId } from "@/utils/marketing/task-node";
import { isActiveStatus } from "@/utils/tasks/status";

import {
  CanvasDialog,
  downloadUrl,
  PlayerDialog,
  PromptDialog,
  takeFileName,
} from "./script-dialogs";
import type { MarketingActions } from "./use-actions";
import { VideoBox } from "./video-box";

/** 旁白问题标签的图标 */
const FLAG_ICON: Record<FlagType, typeof Megaphone> = {
  forbidden: ShieldBan,
  claim: Megaphone,
  brand: Copy,
};

/** 状态徽标 */
function StatusBadge({ script }: { script: Script }) {
  const label = scriptStatusLabel(script.status);
  switch (script.status) {
    case "rendering":
    case "rewriting":
      return (
        <StatusPill tone="running">
          <Loader2 className="animate-spin" />
          {label}
        </StatusPill>
      );
    case "done":
      return (
        <StatusPill tone="success">
          <Check />
          {label}
        </StatusPill>
      );
    case "failed":
      return <StatusPill tone="destructive">{label}</StatusPill>;
    default:
      return <StatusPill tone="muted">{label}</StatusPill>;
  }
}

/**
 * 一张脚本卡：左边成片区（待出片 / 出片中环形进度 / 已出片可播放与切换第几条 / 失败原因），
 * 右边角度标签（跟拍版显示「跟拍」，每镜标「对应参考 镜头 n」）、状态、「已修改」、标题、镜头列表
 * （旁白可改，失焦或回车保存，问题旁白描红）；底部是配乐 · 音色、问题标签、方法小字、提示词、更多菜单
 * （下载成片 / 发到画布精修 / 恢复原稿）和出片按钮（带积分，有问题的旁白改掉才能出片）。
 */
export function ScriptCard({
  project,
  round,
  script,
  angles,
  videoModelLabel,
  available,
  actions,
}: {
  project: Project;
  round: Round;
  script: Script;
  angles: AngleOption[];
  videoModelLabel: string;
  available: number | null;
  actions: MarketingActions;
}) {
  const [picked, setPicked] = useState<number | null>(null);
  const [promptOpen, setPromptOpen] = useState(false);
  const [canvasOpen, setCanvasOpen] = useState(false);
  const [playerOpen, setPlayerOpen] = useState(false);

  const takes = playableTakes(script);
  const current = Math.min(picked ?? takes.length - 1, takes.length - 1);
  const nodeId = marketingNodeId(script.id);
  /** 出片进度：任务库里这个脚本进行中的任务；还没推送时用脚本记录里最近一次的 */
  const progress = useTasksStore((state) => {
    for (const task of Object.values(state.tasks)) {
      if (task.node_id === nodeId && isActiveStatus(task.status)) return task.progress ?? 0;
    }
    return null;
  });

  const busy = isScriptBusy(script.status);
  const render = scriptRenderState(script, available);
  const flagged = flaggedShots(script.flags);
  const types = flagSummary(script.flags);
  const follow = script.kind === "follow";
  const canDownload = script.status === "done" && takes.length > 0;
  const restoring = actions.busy[`script-restore:${script.id}`];
  const rendering = actions.busy[`render:${script.id}`];

  const saveNarration = (index: number, value: string) => {
    const narrations = script.shots.map((shot, i) => (i === index ? value : shot.narration));
    if (narrationsChanged(script, narrations)) void actions.saveNarrations(script, narrations);
  };

  const download = () => {
    const take = takes[current];
    const url = take?.outputs[0]?.url;
    if (url) downloadUrl(url, takeFileName(project, script, current, angles));
  };

  /** 先同步打开空白标签（异步完成后再开会被拦截），拿到画布 ID 再导航过去 */
  const sendToCanvas = async () => {
    const tab = window.open("", "_blank");
    const id = await actions.toCanvas(script);
    if (id) {
      setCanvasOpen(false);
      if (tab) tab.location.href = `/canvas/${id}`;
      else window.open(`/canvas/${id}`, "_blank");
    } else {
      tab?.close();
    }
  };

  return (
    <article
      data-slot="marketing-script-card"
      data-kind={script.kind}
      className={cn(
        "bg-chrome ring-chrome-border relative grid gap-4 rounded-3xl p-4 ring-1 backdrop-blur-xl sm:grid-cols-[auto_1fr]",
        follow && "ring-beam/30",
      )}
    >
      <div className="sm:self-start">
        <VideoBox
          script={script}
          params={round.settings.params}
          posterUrl={project.productAssets[0]?.url ?? ""}
          progress={progress ?? script.takes.at(-1)?.progress ?? null}
          takes={takes}
          current={current}
          onSelectTake={setPicked}
          onPlay={() => setPlayerOpen(true)}
        />
      </div>

      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-1.5">
          <StatusPill tone="muted" className={cn(follow ? "bg-beam/15 text-beam" : "")}>
            {follow && <Copy />}
            {angleName(script.angle, angles)}
          </StatusPill>
          <StatusBadge script={script} />
          {script.edited && <StatusPill tone="muted">已修改</StatusPill>}
        </div>
        <div className="mt-2 text-[14.5px] font-semibold">{script.title}</div>
        <ol className="mt-2 grid gap-1.5">
          {script.shots.map((shot, index) => (
            <li
              key={index}
              className="grid grid-cols-[5.5rem_1fr] items-start gap-2 text-[13px] max-sm:grid-cols-1"
            >
              <div className="text-muted-foreground pt-1 text-xs tabular-nums">
                {shot.start}–{shot.end}s
                <b className="text-foreground/80 block font-medium">
                  {shot.shot} · {shot.camera}
                </b>
                {shot.refShot !== null && (
                  <span
                    title={`对应参考片镜头 ${shot.refShot}`}
                    className="bg-beam/12 text-beam mt-0.5 inline-block rounded-full px-1.5 text-[10.5px] leading-4"
                  >
                    参考 {shot.refShot}
                  </span>
                )}
              </div>
              <div className="min-w-0">
                <div className="text-foreground/85 px-2 pt-1 leading-relaxed">{shot.scene}</div>
                <div className="flex items-center gap-0.5">
                  <Mic className="text-muted-foreground ml-1 size-3.5 shrink-0" />
                  <EditableText
                    value={shot.narration}
                    ariaLabel={`镜头 ${index + 1} 旁白`}
                    placeholder="无旁白"
                    disabled={busy}
                    invalid={flagged.has(index)}
                    onCommit={(value) => saveNarration(index, value)}
                  />
                </div>
              </div>
            </li>
          ))}
        </ol>
      </div>

      <div className="flex flex-wrap items-center gap-1.5 sm:col-span-2">
        <span
          className="text-muted-foreground inline-flex min-w-0 flex-1 items-center gap-1.5 text-xs"
          title={`配乐：${script.music}；旁白音色：${script.voice}`}
        >
          <Music className="size-3.5 shrink-0" />
          <span className="truncate">
            {[script.music, script.voice].filter(Boolean).join(" · ")}
          </span>
        </span>
        {types.map((type) => {
          const Icon = FLAG_ICON[type];
          return (
            <StatusPill
              key={type}
              tone={type === "claim" ? "warning" : "destructive"}
              className="cursor-default"
            >
              <span title={flagReason(script.flags)} className="inline-flex items-center gap-1">
                <Icon />
                {FLAG_NAME[type]}
              </span>
            </StatusPill>
          );
        })}
        {script.method && (
          <span
            title="这份脚本由哪个方法写出（后台按环节绑定）"
            className="text-muted-foreground inline-flex items-center gap-1 text-xs"
          >
            <BookOpen className="size-3.5" />
            方法：{methodLabel(script.method)}
          </span>
        )}
        <Button
          variant="ghost"
          size="xs"
          title="查看格式化后的视频提示词"
          onClick={() => setPromptOpen(true)}
        >
          <CodeXml />
          提示词
        </Button>
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger
            aria-label="更多操作"
            className="hover:bg-chrome-hover focus-visible:ring-ring/50 text-muted-foreground hover:text-foreground grid size-7 place-items-center rounded-full outline-none focus-visible:ring-2 data-popup-open:bg-chrome-hover [&_svg]:size-4"
          >
            <MoreHorizontal />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" sideOffset={6} className="w-44">
            <DropdownMenuItem disabled={!canDownload} onClick={download}>
              <Download />
              下载成片
            </DropdownMenuItem>
            <DropdownMenuItem disabled={busy} onClick={() => setCanvasOpen(true)}>
              <Workflow />
              发到画布精修
            </DropdownMenuItem>
            <DropdownMenuItem
              disabled={busy || !script.edited || restoring}
              onClick={() => void actions.restoreScript(script)}
            >
              <Undo2 />
              恢复原稿
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <Button
          size="xs"
          disabled={render.disabled || rendering}
          title={render.reason || (render.again ? "按当前脚本再出一条" : "按当前脚本出片")}
          onClick={() => void actions.render(script)}
        >
          {rendering ? (
            <Loader2 className="animate-spin" />
          ) : render.again ? (
            <RotateCcw />
          ) : (
            <Play />
          )}
          {render.again ? "重出" : "出片"}
          <span className="inline-flex items-center gap-0.5 tabular-nums opacity-70">
            ✦{script.renderCredits}
          </span>
        </Button>
      </div>

      <PromptDialog
        script={script}
        videoModel={round.settings.videoModel}
        videoModelLabel={videoModelLabel}
        angles={angles}
        open={promptOpen}
        onOpenChange={setPromptOpen}
      />
      <CanvasDialog
        project={project}
        round={round}
        script={script}
        angles={angles}
        busy={!!actions.busy[`canvas:${script.id}`]}
        open={canvasOpen}
        onOpenChange={setCanvasOpen}
        onConfirm={() => void sendToCanvas()}
      />
      <PlayerDialog
        take={takes[current]}
        index={Math.max(current, 0)}
        params={round.settings.params}
        fileName={takeFileName(project, script, Math.max(current, 0), angles)}
        open={playerOpen}
        onOpenChange={setPlayerOpen}
      />
    </article>
  );
}
