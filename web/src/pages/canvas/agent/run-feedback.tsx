import { useState, type ReactNode } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { Copy, Loader2, ThumbsDown, ThumbsUp } from "lucide-react";
import { toast } from "sonner";

import type { AgentFeedbackRating, AgentFeedbackReason } from "@/api/agent/type";
import type { AgentController } from "@/hooks/use-agent-controller";
import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import {
  FEEDBACK_COMMENT_MAX,
  FEEDBACK_REASONS,
  feedbackNote,
  thumbAction,
  type AgentFeedback,
} from "@/utils/agent/feedback";

/** 点踩时还没提交的草稿 */
export type FeedbackDraft = {
  /** 选中的原因标签，可以一个都不选 */
  reasons: AgentFeedbackReason[];
  /** 补充说明，最多 FEEDBACK_COMMENT_MAX 字 */
  comment: string;
};

/** 反馈条的外观与事件：状态都由外面给，这里只管画和回调（便于单独验证各种状态） */
export type FeedbackBarProps = {
  /** 这一轮已有的评价 */
  feedback: AgentFeedback | undefined;
  /** 正在保存：按钮和表单的提交都禁用 */
  saving: boolean;
  /** 原因和说明的表单是否展开 */
  formOpen: boolean;
  /** 表单草稿 */
  draft: FeedbackDraft;
  onThumb: (rating: AgentFeedbackRating) => void;
  onToggleReason: (reason: AgentFeedbackReason) => void;
  onComment: (text: string) => void;
  onCancelForm: () => void;
  onSubmitForm: () => void;
  /** 同一行左边的内容：改动胶囊 */
  leading?: ReactNode;
  /** 行下方的展开内容：改动明细 */
  below?: ReactNode;
  /** 复制按钮复制的回答；空串不显示复制 */
  copyText?: string;
  /** 历史轮：复制和评价悬停或聚焦才出现（已评价的常显）；最后一轮常显 */
  quiet?: boolean;
};

const THUMB_BASE =
  "text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 grid size-7 place-items-center rounded-lg outline-none transition-colors duration-120 focus-visible:ring-2 disabled:pointer-events-none disabled:opacity-40 aria-pressed:text-foreground aria-pressed:bg-foreground/10 [&_svg]:size-3.5";

/** 复制这一轮的回答 */
function CopyButton({ text }: { text: string }) {
  return (
    <motion.button
      type="button"
      aria-label="复制"
      title="复制"
      whileTap={TAP}
      onClick={() =>
        void navigator.clipboard
          .writeText(text)
          .then(() => toast.success("已复制"))
          .catch(() => toast.error("复制失败"))
      }
      className={THUMB_BASE}
    >
      <Copy />
    </motion.button>
  );
}

/** 一个 👍/👎 按钮：选中态用 aria-pressed，保存中禁用 */
function Thumb({
  rating,
  pressed,
  disabled,
  title,
  onClick,
}: {
  rating: AgentFeedbackRating;
  pressed: boolean;
  disabled: boolean;
  title: string;
  onClick: () => void;
}) {
  const Icon = rating === 1 ? ThumbsUp : ThumbsDown;
  return (
    <motion.button
      type="button"
      data-slot="run-feedback-thumb"
      data-rating={rating}
      aria-pressed={pressed}
      aria-label={title}
      title={title}
      disabled={disabled}
      whileTap={disabled ? undefined : TAP}
      onClick={onClick}
      className={THUMB_BASE}
    >
      <Icon className={cn(pressed && "fill-current")} />
    </motion.button>
  );
}

/**
 * 每轮运行末尾的底栏（设计稿 6.12）：一行里左边是改动胶囊，右边是复制、👍、👎；只在运行结束后出现，进行中不显示。
 * 点踩在下方展开原因标签（多选，可不选）和可选的说明；评价后在按钮左边留一行小字。
 */
export function FeedbackBar({
  feedback,
  saving,
  formOpen,
  draft,
  onThumb,
  onToggleReason,
  onComment,
  onCancelForm,
  onSubmitForm,
  leading,
  below,
  copyText = "",
  quiet = false,
}: FeedbackBarProps) {
  const reduce = useReducedMotion();
  const rating = feedback?.rating;
  const hidden = quiet && !feedback && !formOpen;
  return (
    <div data-slot="run-feedback" className="group/foot flex flex-col gap-2 text-xs">
      <div className="flex min-h-7 items-center gap-1">
        {leading}
        <div
          className={cn(
            "ml-auto flex items-center gap-0.5 transition-opacity duration-120",
            hidden && "opacity-0 group-focus-within/foot:opacity-100 group-hover/foot:opacity-100",
          )}
        >
          {feedback && !formOpen && (
            <span data-slot="run-feedback-note" className="text-muted-foreground mr-1">
              {feedbackNote(feedback)}
            </span>
          )}
          {copyText && <CopyButton text={copyText} />}
          <Thumb
            rating={1}
            pressed={rating === 1}
            disabled={saving}
            title="有帮助"
            onClick={() => onThumb(1)}
          />
          <Thumb
            rating={-1}
            pressed={rating === -1}
            disabled={saving}
            title="没帮上忙"
            onClick={() => onThumb(-1)}
          />
        </div>
      </div>
      {below}
      <AnimatePresence initial={false}>
        {formOpen && (
          <motion.div
            data-slot="run-feedback-form"
            initial={reduce ? { opacity: 0 } : { opacity: 0, y: -4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, transition: { duration: DURATION.fast * 0.7 } }}
            transition={{ duration: DURATION.fast, ease: EASE_OUT }}
            className="agent-inset from-foreground/4 to-foreground/2 flex flex-col gap-2 rounded-xl bg-linear-to-b p-2.5"
          >
            <div className="flex flex-wrap gap-1.5">
              {FEEDBACK_REASONS.map((r) => {
                const on = draft.reasons.includes(r.key);
                return (
                  <button
                    key={r.key}
                    type="button"
                    aria-pressed={on}
                    disabled={saving}
                    onClick={() => onToggleReason(r.key)}
                    className="text-muted-foreground hover:text-foreground ring-chrome-border focus-visible:ring-node-ring/60 aria-pressed:bg-foreground/10 aria-pressed:text-foreground h-6 rounded-full px-2.5 text-xs ring-1 outline-none transition-colors duration-120 focus-visible:ring-2 disabled:opacity-60"
                  >
                    {r.label}
                  </button>
                );
              })}
            </div>
            <textarea
              rows={2}
              maxLength={FEEDBACK_COMMENT_MAX}
              value={draft.comment}
              disabled={saving}
              placeholder={`哪里不对？（可选，≤${FEEDBACK_COMMENT_MAX} 字）`}
              aria-label="补充说明"
              onChange={(e) => onComment(e.target.value)}
              className="ring-chrome-border placeholder:text-muted-foreground focus-visible:ring-node-ring/60 resize-none rounded-lg bg-transparent px-2 py-1.5 text-[13px] ring-1 outline-none focus-visible:ring-2 disabled:opacity-60"
            />
            <div className="flex items-center justify-end gap-1.5">
              <span className="text-muted-foreground mr-auto">原因可以不选</span>
              <button
                type="button"
                disabled={saving}
                onClick={onCancelForm}
                className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-node-ring/60 h-7 rounded-lg px-2.5 outline-none transition-colors duration-120 focus-visible:ring-2 disabled:opacity-60"
              >
                取消
              </button>
              <button
                type="button"
                disabled={saving}
                onClick={onSubmitForm}
                className="bg-foreground text-background focus-visible:ring-node-ring/60 flex h-7 items-center gap-1 rounded-lg px-3 font-medium outline-none focus-visible:ring-2 disabled:opacity-60"
              >
                {saving && <Loader2 className="size-3.5 animate-spin motion-reduce:animate-none" />}
                {saving ? "提交中…" : "提交"}
              </button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

const EMPTY_DRAFT: FeedbackDraft = { reasons: [], comment: "" };

/**
 * 一轮运行的反馈位：评价来自会话状态（事件回放 + 本地乐观更新），表单草稿和保存中是本地状态。
 * 👍 直接提交；👎 先展开原因再提交；再点已选中的按钮取消。保存失败的提示和回滚由 store 处理。
 */
export function RunFeedback({
  runId,
  ctl,
  ...slots
}: { runId: string; ctl: AgentController } & Pick<
  FeedbackBarProps,
  "leading" | "below" | "copyText" | "quiet"
>) {
  const feedback = ctl.state.feedback[runId];
  const [saving, setSaving] = useState(false);
  const [formOpen, setFormOpen] = useState(false);
  const [draft, setDraft] = useState<FeedbackDraft>(EMPTY_DRAFT);

  /** 保存中不再接受新的点击：连点只会发一次请求 */
  const guarded = async (job: () => Promise<boolean>) => {
    if (saving) return false;
    setSaving(true);
    try {
      return await job();
    } finally {
      setSaving(false);
    }
  };

  const onThumb = (rating: AgentFeedbackRating) => {
    const action = thumbAction(feedback, rating);
    if (action === "toggle-form") {
      // 改选时带上之前的原因和说明，方便微调
      if (!formOpen && feedback?.rating === -1) setDraft({ ...feedback });
      setFormOpen((open) => !open);
      return;
    }
    setFormOpen(false);
    setDraft(EMPTY_DRAFT);
    void guarded(() =>
      action === "cancel" ? ctl.clearRunRating(runId) : ctl.rateRun(runId, { rating: 1 }),
    );
  };

  const onSubmitForm = () => {
    void guarded(() =>
      ctl.rateRun(runId, { rating: -1, reasons: draft.reasons, comment: draft.comment.trim() }),
    ).then((ok) => {
      // 失败时表单保持展开，用户的草稿不丢
      if (ok) {
        setFormOpen(false);
        setDraft(EMPTY_DRAFT);
      }
    });
  };

  return (
    <FeedbackBar
      {...slots}
      feedback={feedback}
      saving={saving}
      formOpen={formOpen}
      draft={draft}
      onThumb={onThumb}
      onToggleReason={(reason) =>
        setDraft((d) => ({
          ...d,
          reasons: d.reasons.includes(reason)
            ? d.reasons.filter((r) => r !== reason)
            : [...d.reasons, reason],
        }))
      }
      onComment={(comment) => setDraft((d) => ({ ...d, comment }))}
      onCancelForm={() => {
        setFormOpen(false);
        setDraft(EMPTY_DRAFT);
      }}
      onSubmitForm={onSubmitForm}
    />
  );
}
