import { motion } from "motion/react";
import {
  Bot,
  Clapperboard,
  Info,
  MoreHorizontal,
  Pencil,
  RotateCw,
  Sparkles,
  Workflow,
} from "lucide-react";

import { MediaBlock } from "@/components/conversation/media-block";
import { SoonTip } from "@/components/home/soon";
import { MODE_NAME } from "@/constants/creation";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useComposerStore } from "@/store/composer";
import { placeholderBackground } from "@/utils/home/placeholder";
import type { ConversationRecord } from "@/types";

/** 记录底部操作按钮的公共样式 */
const ACTION =
  "bg-muted hover:bg-foreground/10 focus-visible:ring-ring/50 inline-flex h-8 items-center gap-1.5 rounded-[10px] px-3 text-[13px] font-medium outline-none focus-visible:ring-3 disabled:opacity-60";

/**
 * 对话里的一条生成记录（参照即梦对话页）：
 * 参考图叠在左边，右边是「模式 | 模型 | 时长 | 清晰度 | 详细信息」，下面依次是提示词、Agent 回复、结果和操作。
 * 「重新编辑」是真的：把模式和提示词填回输入卡片；
 * 再次生成、下一步、在画布中打开、更多、详细信息都要等接口，先禁用。
 * @param record 记录
 */
export function ConversationRecordView({ record }: { record: ConversationRecord }) {
  const setMode = useComposerStore((state) => state.setMode);
  const fill = useComposerStore((state) => state.fill);
  const pending = record.progress !== null;

  return (
    <article data-slot="conversation-record" className="mb-8 min-w-0">
      <div className="mb-2.5 flex items-end gap-3.5">
        <div className="flex pt-1 pl-0.5">
          {record.refHues.length > 0 ? (
            record.refHues.map((hue, index) => (
              <span
                key={index}
                aria-hidden
                style={{ background: placeholderBackground(hue) }}
                className={cn(
                  "border-background h-11.5 w-8.5 rounded-md border-2",
                  index === 0 ? "-rotate-6" : "-ml-2 rotate-5",
                )}
              />
            ))
          ) : (
            <span
              aria-hidden
              className="bg-muted text-muted-foreground border-background grid h-11.5 w-8.5 -rotate-6 place-items-center rounded-md border-2"
            >
              {record.mode === "agent" ? <Bot className="size-3.5" /> : <Sparkles className="size-3.5" />}
            </span>
          )}
        </div>
        <div className="text-muted-foreground flex flex-wrap items-center pb-0.5 text-[13px]">
          <span className="text-foreground font-medium">{MODE_NAME[record.mode]}</span>
          {record.meta.map((item) => (
            <span key={item} className="before:bg-border before:mx-2 before:inline-block before:h-2.5 before:w-px before:align-[-1px]">
              {item}
            </span>
          ))}
          <span className="before:bg-border before:mx-2 before:inline-block before:h-2.5 before:w-px before:align-[-1px]">
            <SoonTip className="inline-flex cursor-not-allowed">
              <button type="button" disabled className="inline-flex items-center gap-0.5">
                详细信息
                <Info className="size-3.5" />
              </button>
            </SoonTip>
          </span>
        </div>
      </div>

      <p className="mb-2.5 max-w-190 text-sm leading-[1.65]">{record.prompt}</p>

      {record.reply && !pending && (
        <div className="text-muted-foreground mb-2.5 flex max-w-190 items-start gap-2.5 text-sm leading-[1.65]">
          <span className="bg-muted text-beam mt-px grid size-6 shrink-0 place-items-center rounded-lg">
            <Bot className="size-3.5" />
          </span>
          <span>{record.reply}</span>
        </div>
      )}

      <div className="flex flex-wrap gap-2">
        {record.results.map((result, index) => (
          <MediaBlock key={index} result={result} progress={record.progress} />
        ))}
      </div>

      {!pending && (
        <div className="mt-2.5 flex flex-wrap gap-2">
          {record.next && (
            <>
              <SoonTip>
                <button
                  type="button"
                  disabled
                  className={cn(ACTION, "bg-foreground text-background hover:bg-foreground")}
                >
                  <Clapperboard className="size-4" />
                  {record.next}
                </button>
              </SoonTip>
              <SoonTip>
                <button type="button" disabled className={ACTION}>
                  <Workflow className="size-4" />
                  在画布中打开
                </button>
              </SoonTip>
            </>
          )}
          <motion.button
            type="button"
            whileTap={TAP}
            onClick={() => {
              setMode(record.mode);
              fill(record.prompt);
            }}
            className={ACTION}
          >
            <Pencil className="size-4" />
            重新编辑
          </motion.button>
          <SoonTip>
            <button type="button" disabled className={ACTION}>
              <RotateCw className="size-4" />
              再次生成
            </button>
          </SoonTip>
          <SoonTip>
            <button
              type="button"
              disabled
              aria-label="更多：下载、拖进画布、删除"
              className={cn(ACTION, "w-8 justify-center px-0")}
            >
              <MoreHorizontal className="size-4" />
            </button>
          </SoonTip>
        </div>
      )}
    </article>
  );
}
