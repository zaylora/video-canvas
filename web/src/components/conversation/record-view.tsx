import { useState } from "react";
import { motion } from "motion/react";
import {
  CircleStop,
  Copy,
  Download,
  Loader2,
  Music,
  MoreHorizontal,
  Pencil,
  RotateCw,
  Sparkle,
  Sparkles,
  Trash2,
  Workflow,
} from "lucide-react";
import { toast } from "sonner";

import type { RecordDto } from "@/api/conversation/type";
import type { TaskOutput } from "@/api/generation-task/type";
import type { RefKind } from "@/api/model/type";
import { ConfirmDialog } from "@/components/admin-ui/confirm-dialog";
import { PreviewDialog } from "@/components/conversation/preview-dialog";
import { ResultCell } from "@/components/conversation/result-cell";
import { SoonTip } from "@/components/home/soon";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { MODE_NAME } from "@/constants/creation";
import { isModelOffline, useRecordActions, useRecordTasks } from "@/hooks/use-record-actions";
import { useRemoteModels } from "@/hooks/use-models";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useConversationRecordsStore } from "@/store/conversation-records";
import { downloadMedia } from "@/utils/canvas/download";
import { recordActive, recordCredits } from "@/utils/conversation/cell";
import { restoreComposer } from "@/utils/conversation/restore";
import { isCancelableStatus } from "@/utils/tasks/status";
import { useAssetUrls } from "@/hooks/use-asset-urls";
import { paramSummary } from "@/utils/tasks/capabilities";

/** 记录底部操作按钮的公共样式 */
const ACTION =
  "bg-muted hover:bg-foreground/10 focus-visible:ring-ring/50 inline-flex h-8 items-center gap-1.5 rounded-[10px] px-3 text-[13px] font-medium outline-none focus-visible:ring-3 disabled:opacity-60";

/** 竖线分隔的元信息项 */
const META_ITEM =
  "before:bg-border before:mx-2 before:inline-block before:h-2.5 before:w-px before:align-[-1px]";

/** 标题左边最多叠几份参考缩略图 */
const MAX_THUMBS = 3;

/**
 * 记录头部左侧：有参考素材时叠放缩略图（超过 3 份只画前 3 份），没有就是一个星星占位。
 * 图片和视频取素材地址画缩略（视频取首帧），音频画音符；地址还没取到或素材已被清理时先显示灰块。
 * 参考素材从记录的输入快照里还原（restoreComposer），和「重新编辑」用的是同一份解析。
 */
function RefThumbs({ refs }: { refs: Array<{ kind: RefKind; id: number }> }) {
  const shown = refs.slice(0, MAX_THUMBS);
  const urls = useAssetUrls(shown.map((ref) => ref.id));
  if (shown.length === 0) {
    return (
      <span
        aria-hidden
        className="bg-muted text-muted-foreground border-background grid h-11.5 w-8.5 -rotate-6 place-items-center rounded-md border-2"
      >
        <Sparkles className="size-3.5" />
      </span>
    );
  }
  return (
    <div className="flex pt-1 pl-0.5" title={`参考素材 ${refs.length} 个`}>
      {shown.map((ref, index) => (
        <span
          key={`${ref.kind}-${ref.id}`}
          className={cn(
            "bg-muted border-background grid h-11.5 w-8.5 shrink-0 place-items-center overflow-hidden rounded-md border-2",
            index === 0 ? "-rotate-6" : index % 2 === 1 ? "-ml-2 rotate-5" : "-ml-2 -rotate-3",
          )}
        >
          {ref.kind === "audio" ? (
            <Music className="text-muted-foreground size-3.5" />
          ) : ref.kind === "video" ? (
            urls[index] && (
              <video
                src={urls[index]}
                muted
                preload="metadata"
                className="size-full object-cover"
              />
            )
          ) : (
            urls[index] && (
              <img
                src={urls[index]}
                alt="参考图"
                draggable={false}
                className="size-full object-cover"
              />
            )
          )}
        </span>
      ))}
    </div>
  );
}

/** 提示词超过这个长度默认折叠成三行 */
const LONG_PROMPT = 140;

/**
 * 对话里的一条生成记录：参考图数量 + 「模式 | 模型 | 参数 | 积分」，提示词，结果格子，操作。
 * 操作：重新编辑（填回输入卡片）、再次生成（用同一份快照新提交一条）、取消进行中的任务、
 * 更多（复制提示词、下载全部、发到画布、删除）。发到画布要等阶段 2，先禁用。
 * @param record 记录
 * @param conversationId 所在对话 ID
 * @param onUseAsRef 把某个结果用作参考图
 */
export function RecordView({
  record,
  conversationId,
  onUseAsRef,
}: {
  record: RecordDto;
  conversationId: string;
  onUseAsRef: (output: TaskOutput) => void;
}) {
  const tasks = useRecordTasks(record);
  const { models, status } = useRemoteModels(record.kind);
  const actions = useRecordActions(conversationId);
  const [preview, setPreview] = useState<TaskOutput | null>(null);
  const [expanded, setExpanded] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const active = recordActive(tasks);
  /** 转存中的任务不能取消，只有排队 / 生成中的才给「取消」 */
  const cancelable = tasks.some((task) => isCancelableStatus(task.status));
  const credits = recordCredits(tasks);
  const model = models.find((item) => item.key === record.modelId);
  const offline = isModelOffline(
    record,
    status === "ready" ? models.map((item) => item.key) : null,
  );
  const succeeded = tasks.flatMap((task) =>
    task.status === "succeeded" ? (task.outputs ?? []) : [],
  );
  const refs = restoreComposer(record).refs;
  const refCount = refs.length;
  const long = record.prompt.length > LONG_PROMPT;
  const summary = model ? paramSummary(model.capabilities, record.input) : "";

  const remove = async () => {
    setDeleting(true);
    try {
      await useConversationRecordsStore.getState().remove(conversationId, record.id, active);
      setConfirmDelete(false);
    } catch {
      // 提示由请求层统一弹，确认框保持打开，用户可以重试
    } finally {
      setDeleting(false);
    }
  };

  const downloadAll = () => {
    succeeded.forEach((output, index) => {
      if (output.url)
        void downloadMedia(output.url, `${record.prompt.slice(0, 20) || "生成结果"}-${index + 1}`);
    });
  };

  return (
    <article data-slot="conversation-record" className="mb-8 min-w-0">
      <div className="mb-2.5 flex items-end gap-3.5">
        <RefThumbs refs={refs} />
        <div className="text-muted-foreground flex flex-wrap items-center pb-0.5 text-[13px]">
          <span className="text-foreground font-medium">{MODE_NAME[record.kind]}</span>
          <span className={META_ITEM}>{model?.label ?? record.modelId}</span>
          {offline && (
            <span className={cn(META_ITEM, "text-amber-600 dark:text-amber-400")}>已下线</span>
          )}
          {summary && <span className={META_ITEM}>{summary}</span>}
          {refCount > 0 && <span className={META_ITEM}>参考 {refCount} 个</span>}
          {tasks.length > 0 && (
            <span
              className={cn(META_ITEM, "inline-flex items-center gap-0.5")}
              title={credits.frozen ? "已冻结，完成后按实际结算" : "实际扣费，失败已退回"}
            >
              <Sparkle className="size-3" />
              {credits.credits}
              {credits.frozen ? "（冻结）" : ""}
            </span>
          )}
        </div>
      </div>

      <p
        className={cn(
          "mb-1 max-w-190 text-sm leading-[1.65] whitespace-pre-wrap",
          long && !expanded && "line-clamp-3",
        )}
      >
        {record.prompt}
      </p>
      {long && (
        <button
          type="button"
          onClick={() => setExpanded((value) => !value)}
          className="text-muted-foreground hover:text-foreground mb-2 text-xs"
        >
          {expanded ? "收起" : "展开"}
        </button>
      )}

      <div className="mt-1.5 flex flex-wrap gap-2">
        {Array.from({ length: record.count }, (_, index) => (
          <ResultCell
            key={index}
            record={record}
            index={index}
            onPreview={setPreview}
            onRetry={() => void actions.retryCell(record)}
            onUseAsRef={onUseAsRef}
          />
        ))}
      </div>

      <div className="mt-2.5 flex flex-wrap gap-2">
        <motion.button
          type="button"
          whileTap={TAP}
          onClick={() => actions.reEdit(record)}
          className={ACTION}
        >
          <Pencil className="size-4" />
          重新编辑
        </motion.button>
        <motion.button
          type="button"
          whileTap={TAP}
          disabled={offline || actions.busy}
          title={offline ? "原模型已下线，请重新编辑" : "用同样的设置再生成一次，作为新记录追加"}
          onClick={() => void actions.regenerate(record)}
          className={ACTION}
        >
          {actions.busy ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <RotateCw className="size-4" />
          )}
          再次生成
        </motion.button>
        {cancelable && (
          <motion.button
            type="button"
            whileTap={TAP}
            disabled={actions.busy}
            onClick={() => void actions.cancel(record)}
            className={ACTION}
          >
            <CircleStop className="size-4" />
            取消
          </motion.button>
        )}
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger
            render={<motion.button type="button" whileTap={TAP} />}
            aria-label="更多：复制、下载、发到画布、删除"
            className={cn(ACTION, "w-8 justify-center px-0")}
          >
            <MoreHorizontal className="size-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent side="top" sideOffset={6} className="w-48">
            <DropdownMenuItem
              onClick={() => {
                void navigator.clipboard
                  ?.writeText(record.prompt)
                  .then(() => toast.success("已复制提示词"));
              }}
            >
              <Copy />
              复制提示词
            </DropdownMenuItem>
            <DropdownMenuItem disabled={succeeded.length === 0} onClick={downloadAll}>
              <Download />
              下载全部
            </DropdownMenuItem>
            <SoonTip side="right" className="block cursor-not-allowed">
              <DropdownMenuItem disabled>
                <Workflow />
                发到画布
              </DropdownMenuItem>
            </SoonTip>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onClick={() => setConfirmDelete(true)}>
              <Trash2 />
              删除记录
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <PreviewDialog output={preview} title={record.prompt} onClose={() => setPreview(null)} />
      <ConfirmDialog
        open={confirmDelete}
        title="删除这条记录？"
        description={
          active
            ? "其中进行中的生成会被取消，积分退回。已经生成的素材仍保留在资产里。"
            : "已经生成的素材仍保留在资产里。"
        }
        confirmLabel="删除"
        destructive
        busy={deleting}
        onConfirm={() => void remove()}
        onCancel={() => setConfirmDelete(false)}
      />
    </article>
  );
}
