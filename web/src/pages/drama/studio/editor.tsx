import { motion } from "motion/react";
import { CircleX, Loader2, Trash2 } from "lucide-react";

import { StatusPill } from "@/components/drama/status-pill";
import { Button } from "@/components/ui/button";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { hasVideo, latestError } from "@/utils/drama/segments";

import { PromptPane } from "./prompt-pane";
import { RefsBar } from "./refs-bar";
import { useStudio } from "./use-studio";

/** 右侧检查器：撑满所在分栏（宽度由用户拖动），自己滚动 */
const INSPECTOR = "bg-card h-full w-full min-w-0 overflow-y-auto";

/** 中间编辑区：片段标题、参考素材条、提示词、失败提示（时长、视频模型和生成按钮在顶栏） */
export function Editor() {
  const { selected, actions, segmentsStatus, no } = useStudio();

  if (!selected) {
    return (
      <section className={INSPECTOR}>
        <p className="text-muted-foreground mt-20 px-6 text-center text-sm">
          {segmentsStatus === "loading"
            ? "加载中…"
            : segmentsStatus === "error"
              ? "片段读取失败，请刷新页面重试"
              : "这一集还没有片段，点下方时间线末尾的「+」新建一个"}
        </p>
      </section>
    );
  }

  const error = latestError(selected);
  const out = hasVideo(selected);

  return (
    <section aria-label={`第 ${no} 集 片段 ${selected.seq + 1}`} className={INSPECTOR}>
      <motion.div
        key={selected.id}
        initial={{ opacity: 0, y: 6 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: DURATION.base, ease: EASE_OUT }}
        className="grid gap-4 px-4 pt-4 pb-8"
      >
        <div className="flex items-baseline gap-2">
          <h2 className="font-mono text-[22px] font-semibold tracking-tight">
            片段 {String(selected.seq + 1).padStart(2, "0")}
          </h2>
          <span className="flex-1" />
          {selected.status === "error" && <StatusPill tone="destructive">生成失败</StatusPill>}
          {selected.status === "running" && (
            <StatusPill tone="running">
              <Loader2 className="animate-spin" />
              生成中
            </StatusPill>
          )}
          {selected.status !== "running" && selected.status !== "error" && out && (
            <StatusPill tone="success">已出视频</StatusPill>
          )}
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="删除这个片段"
            title="删除这个片段"
            onClick={() => void actions.remove(selected)}
          >
            <Trash2 />
          </Button>
        </div>

        <RefsBar />

        <PromptPane key={selected.id} segment={selected} />

        {error && (
          <div
            role="alert"
            className="bg-destructive/8 border-destructive/25 flex items-start gap-2 rounded-lg border px-3 py-2 text-sm"
          >
            <CircleX className="text-destructive mt-0.5 size-4 shrink-0" />
            <div>{error}</div>
          </div>
        )}
      </motion.div>
    </section>
  );
}
