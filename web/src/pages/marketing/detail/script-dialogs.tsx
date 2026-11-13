import { useEffect, useState } from "react";
import { Copy, Film, Image as ImageIcon, Loader2, Video } from "lucide-react";
import { toast } from "sonner";

import { getMarketingPrompt } from "@/api/marketing";
import type { AngleOption, Project, Round, Script } from "@/api/marketing/type";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { angleName } from "@/utils/marketing/options";

/** 触发浏览器下载一个地址（跨域地址会在新标签打开，由浏览器决定下载还是播放） */
export function downloadUrl(url: string, name: string) {
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.target = "_blank";
  link.rel = "noopener";
  link.click();
}

/** 成片文件名：营销片标题 + 角度 + 第几条 */
export function takeFileName(
  project: Project,
  script: Script,
  index: number,
  angles: AngleOption[],
) {
  return `${project.title}-${angleName(script.angle, angles)}-${index + 1}.mp4`;
}

/** 视频提示词预览：由代码里的格式化器按视频模型拼装，与技能无关；可复制 */
export function PromptDialog({
  script,
  videoModel,
  videoModelLabel,
  angles,
  open,
  onOpenChange,
}: {
  script: Script;
  /** 本轮视频模型 key */
  videoModel: string;
  videoModelLabel: string;
  angles: AngleOption[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [state, setState] = useState<
    | { status: "loading" }
    | { status: "error" }
    | { status: "ready"; format: string; prompt: string }
  >({ status: "loading" });

  useEffect(() => {
    if (!open) return;
    let alive = true;
    setState({ status: "loading" });
    getMarketingPrompt(script.id, videoModel)
      .then((res) => alive && setState({ status: "ready", format: res.format, prompt: res.prompt }))
      .catch(() => alive && setState({ status: "error" }));
    return () => {
      alive = false;
    };
    // 提示词随脚本内容（revision）变化而变化
  }, [open, script.id, script.revision, videoModel]);

  const copy = async () => {
    if (state.status !== "ready") return;
    try {
      await navigator.clipboard.writeText(state.prompt);
      toast.success("已复制");
    } catch {
      toast.error("复制失败，请手动选中复制");
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>视频提示词</DialogTitle>
          <DialogDescription>
            {angleName(script.angle, angles)} · 格式化器「
            {state.status === "ready" && state.format === "seedance"
              ? "seedance 分区块"
              : "通用段落"}
            」按 {videoModelLabel} 拼装，与技能无关
          </DialogDescription>
        </DialogHeader>
        <div className="bg-muted/60 max-h-[55vh] min-h-32 overflow-auto rounded-xl p-3 font-mono text-[12.5px] leading-relaxed whitespace-pre-wrap">
          {state.status === "loading" && (
            <span className="text-muted-foreground inline-flex items-center gap-2">
              <Loader2 className="size-3.5 animate-spin" />
              拼装中
            </span>
          )}
          {state.status === "error" && <span className="text-destructive">提示词加载失败</span>}
          {state.status === "ready" && state.prompt}
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            size="sm"
            disabled={state.status !== "ready"}
            onClick={() => void copy()}
          >
            <Copy />
            复制
          </Button>
          <Button size="sm" onClick={() => onOpenChange(false)}>
            好的
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** 发到画布确认：写清新画布的名字和会放进去的素材、镜头、成片数量 */
export function CanvasDialog({
  project,
  round,
  script,
  angles,
  busy,
  open,
  onOpenChange,
  onConfirm,
}: {
  project: Project;
  round: Round;
  script: Script;
  angles: AngleOption[];
  busy: boolean;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}) {
  const materials =
    project.productAssets.length + (project.modelAsset ? 1 : 0) + (round.refVideo ? 1 : 0);
  const hasFinal = script.takes.some((take) => take.outputs.length > 0);
  return (
    <Dialog open={open} onOpenChange={(next) => !busy && onOpenChange(next)}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>发到画布</DialogTitle>
          <DialogDescription>
            新建画布「{project.title} · {angleName(script.angle, angles)}」
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-wrap gap-2 text-[13px]">
          <span className="bg-chrome-hover ring-chrome-border inline-flex h-7 items-center gap-1.5 rounded-full px-3 ring-1">
            <ImageIcon className="size-3.5" />
            素材 {materials}
          </span>
          <span className="bg-chrome-hover ring-chrome-border inline-flex h-7 items-center gap-1.5 rounded-full px-3 ring-1">
            <Video className="size-3.5" />
            镜头 {script.shots.length}
          </span>
          {hasFinal && (
            <span className="bg-chrome-hover ring-chrome-border inline-flex h-7 items-center gap-1.5 rounded-full px-3 ring-1">
              <Film className="size-3.5" />
              成片 1
            </span>
          )}
        </div>
        <DialogFooter>
          <Button variant="ghost" size="sm" disabled={busy} onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button size="sm" disabled={busy} onClick={onConfirm}>
            {busy && <Loader2 className="animate-spin" />}
            新建并打开
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
