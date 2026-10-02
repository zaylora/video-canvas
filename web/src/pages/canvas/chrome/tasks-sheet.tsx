import { useEffect, useMemo, useState } from "react";
import { useReactFlow } from "@xyflow/react";
import { History, LoaderCircle } from "lucide-react";

import { getGenerationTasksByIds } from "@/api/generation-task";
import type { TaskStatus, TaskView } from "@/api/generation-task/type";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { cn } from "@/lib/utils";
import { useTasksStore } from "@/store/tasks";
import type { CanvasEdge, CanvasNode } from "@/types";
import { taskKey } from "@/utils/tasks/status";

import { useFocusNode } from "./use-focus-node";

const STATUS: Record<TaskStatus, { label: string; tone: string }> = {
  pending: { label: "等待中", tone: "text-muted-foreground" },
  queued: { label: "排队中", tone: "text-muted-foreground" },
  running: { label: "生成中", tone: "text-status-running" },
  finalizing: { label: "即将完成", tone: "text-status-running" },
  succeeded: { label: "已完成", tone: "text-status-success" },
  failed: { label: "失败", tone: "text-destructive" },
  canceled: { label: "已取消", tone: "text-muted-foreground" },
  expired: { label: "已超时", tone: "text-destructive" },
};

const KIND: Record<string, string> = { video: "视频", image: "图片", audio: "音频", text: "文本" };

const timeFormat = new Intl.DateTimeFormat("zh-CN", {
  month: "numeric",
  day: "numeric",
  hour: "2-digit",
  minute: "2-digit",
});

/** 画布里节点（含历史版本）用过的全部任务 id */
function collectTaskIds(nodes: CanvasNode[]) {
  const ids = new Set<string>();
  for (const node of nodes) {
    if (node.data.taskId) ids.add(node.data.taskId);
    for (const output of node.data.outputs ?? []) if (output.taskId) ids.add(output.taskId);
  }
  return [...ids];
}

function TaskList({ onPicked }: { onPicked: () => void }) {
  const { getNodes } = useReactFlow<CanvasNode, CanvasEdge>();
  const focusNode = useFocusNode();
  const [ids] = useState(() => collectTaskIds(getNodes()));
  const [loading, setLoading] = useState(ids.length > 0);
  const tasks = useTasksStore((state) => state.tasks);

  useEffect(() => {
    if (ids.length === 0) return;
    let active = true;
    void getGenerationTasksByIds(ids)
      .then((views) => useTasksStore.getState().upsertMany(views))
      .catch(() => undefined)
      .finally(() => active && setLoading(false));
    return () => {
      active = false;
    };
  }, [ids]);

  const list = useMemo(
    () =>
      ids
        .map((id) => tasks[taskKey(id)])
        .filter((view): view is TaskView => !!view)
        .sort((a, b) => b.created_at.localeCompare(a.created_at)),
    [ids, tasks],
  );

  if (loading && list.length === 0) {
    return (
      <div className="text-muted-foreground flex flex-1 items-center justify-center gap-2 text-sm">
        <LoaderCircle className="size-4 animate-spin" />
        正在加载任务
      </div>
    );
  }

  if (list.length === 0) {
    return (
      <div className="text-muted-foreground flex flex-1 flex-col items-center justify-center gap-3 p-8 text-center text-sm">
        <History className="size-8 opacity-40" />
        这张画布还没有生成过内容。
      </div>
    );
  }

  return (
    <ul className="flex flex-col gap-1 overflow-y-auto px-2 pb-4">
      {list.map((view) => {
        const node = getNodes().find((item) => item.id === view.node_id);
        const status = STATUS[view.status];
        return (
          <li key={view.id}>
            <button
              type="button"
              disabled={!node}
              className="hover:bg-accent focus-visible:bg-accent flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-left text-sm outline-none disabled:opacity-60"
              onClick={() => {
                if (focusNode(view.node_id)) onPicked();
              }}
            >
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="truncate font-medium">
                  {node?.data.label ?? "已删除的节点"}
                  <span className="text-muted-foreground ml-1.5 text-xs font-normal">
                    {KIND[view.kind] ?? view.kind}
                  </span>
                </span>
                <span className="text-muted-foreground truncate text-xs">
                  {timeFormat.format(new Date(view.created_at))} · {view.model_id}
                </span>
              </span>
              <span className="flex shrink-0 flex-col items-end gap-0.5 text-xs">
                <span className={cn("font-medium", status.tone)}>{status.label}</span>
                <span className="text-muted-foreground font-mono tabular-nums">
                  {view.charged_credits ?? view.credits} 积分
                </span>
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}

export function TasksSheet({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="left" className="w-96 gap-0 sm:max-w-96">
        <SheetHeader>
          <SheetTitle>任务历史</SheetTitle>
          <SheetDescription>这张画布上的生成记录，点一条定位到节点</SheetDescription>
        </SheetHeader>
        {open && <TaskList onPicked={() => onOpenChange(false)} />}
      </SheetContent>
    </Sheet>
  );
}
