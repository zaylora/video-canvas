import { useState } from "react";
import { FilePenLine } from "lucide-react";

import type { EpisodeSummary, UnitKind } from "@/api/drama/type";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { sourceRangeLabel } from "@/utils/drama/episodes";

/** 可以直接改的两项：开场钩子、结尾悬念（梗概只读） */
export type MetaField = "hook" | "cliffhanger";

type MetaEpisode = Pick<EpisodeSummary, "synopsis" | "hook" | "cliffhanger">;

const LABELS: Record<"synopsis" | MetaField, string> = {
  synopsis: "梗概",
  hook: "开场钩子",
  cliffhanger: "结尾悬念",
};

/**
 * 一项可编辑的本集信息：点文字切成文本框，回车或失焦保存（没改动不保存），Shift+回车换行，Esc 取消。
 * 保存失败时留在编辑态，提示由调用方给。
 */
function EditableValue({
  label,
  value,
  onSave,
  className,
}: {
  label: string;
  value: string;
  onSave: (value: string) => Promise<void>;
  className?: string;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const commit = async () => {
    if (draft === null || saving) return;
    const next = draft.trim();
    if (next === value.trim()) {
      setDraft(null);
      return;
    }
    setSaving(true);
    try {
      await onSave(next);
      setDraft(null);
    } catch {
      // 留在编辑态，调用方已提示
    } finally {
      setSaving(false);
    }
  };

  if (draft === null) {
    return (
      <button
        type="button"
        aria-label={`编辑${label}`}
        onClick={() => setDraft(value)}
        className={cn(
          "hover:bg-muted focus-visible:ring-ring/50 -mx-1.5 block w-[calc(100%+0.75rem)] rounded-md px-1.5 text-left transition-colors outline-none focus-visible:ring-2",
          !value && "text-muted-foreground",
          className,
        )}
      >
        {value || "—"}
      </button>
    );
  }
  return (
    <textarea
      // 点开就能直接打字
      // oxlint-disable-next-line jsx-a11y/no-autofocus
      autoFocus
      aria-label={label}
      value={draft}
      disabled={saving}
      rows={2}
      onChange={(event) => setDraft(event.target.value)}
      onBlur={() => void commit()}
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.preventDefault();
          setDraft(null);
        } else if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
          event.preventDefault();
          void commit();
        }
      }}
      className={cn(
        "bg-background ring-ring/50 -mx-1.5 block w-[calc(100%+0.75rem)] resize-y rounded-md px-1.5 py-0.5 ring-2 outline-none",
        className,
      )}
    />
  );
}

/**
 * 本集的梗概 / 开场钩子 / 结尾悬念。
 * 给了 onSave 时钩子和悬念可以直接改（空的也列出来好填）；没给时只列有值的行。
 */
export function MetaFields({
  episode,
  onSave,
  layout = "list",
  skipSynopsis = false,
}: {
  /** 这一集的摘要 */
  episode: MetaEpisode;
  /** 保存一项；不传就是只读 */
  onSave?: (field: MetaField, value: string) => Promise<void>;
  /** list：空状态里的两列表；aside：剧本页右侧页边 */
  layout?: "list" | "aside";
  /** 梗概已在别处显示时不重复列 */
  skipSynopsis?: boolean;
}) {
  const fields = (["synopsis", "hook", "cliffhanger"] as const).filter((field) => {
    if (field === "synopsis") return !skipSynopsis && episode.synopsis.trim() !== "";
    return onSave ? true : episode[field].trim() !== "";
  });
  if (fields.length === 0) return null;

  const valueOf = (field: (typeof fields)[number], textClass: string) =>
    field !== "synopsis" && onSave ? (
      <EditableValue
        label={LABELS[field]}
        value={episode[field]}
        onSave={(value) => onSave(field, value)}
        className={textClass}
      />
    ) : (
      <span className={textClass}>{episode[field]}</span>
    );

  if (layout === "aside") {
    return (
      <>
        {fields.map((field) => (
          <div key={field} className="border-border mt-5 border-l-2 pl-3 first:mt-0">
            <b className="text-muted-foreground block text-[12.5px] font-medium">{LABELS[field]}</b>
            <div className="text-muted-foreground mt-0.5 text-xs leading-5">
              {valueOf(field, "text-xs leading-5")}
            </div>
          </div>
        ))}
      </>
    );
  }
  return (
    <dl className="mt-4 grid w-full max-w-140 grid-cols-[72px_minmax(0,1fr)] gap-x-4 gap-y-2.5 text-left text-sm">
      {fields.map((field) => (
        <div key={field} className="contents">
          <dt className="text-muted-foreground">{LABELS[field]}</dt>
          <dd className="leading-6">{valueOf(field, "leading-6")}</dd>
        </div>
      ))}
    </dl>
  );
}

/**
 * 还没有剧本的集：图标 + 「这一集还没有剧本」+「生成剧本」按钮，下面是规划出来的梗概、钩子、悬念。
 */
export function EmptyScript({
  episode,
  onGenerate,
  onSaveMeta,
}: {
  /** 这一集的摘要 */
  episode: MetaEpisode;
  /** 点「生成剧本」：打开写剧本弹窗并预选这一集 */
  onGenerate: () => void;
  /** 改钩子 / 悬念；不传就是只读 */
  onSaveMeta?: (field: MetaField, value: string) => Promise<void>;
}) {
  return (
    <div data-slot="empty-script" className="mt-12 flex flex-col items-center gap-4 text-center">
      <span className="bg-muted text-muted-foreground grid size-12 place-items-center rounded-2xl">
        <FilePenLine className="size-6" />
      </span>
      <div className="text-[15px] font-medium">这一集还没有剧本</div>
      <Button size="sm" onClick={onGenerate}>
        生成剧本
      </Button>
      <MetaFields episode={episode} onSave={onSaveMeta} />
    </div>
  );
}

/**
 * 剧本页右侧页边的本集信息：梗概、开场钩子、结尾悬念，以及「对应原文：第 N 章」。
 * 什么都没有时返回 null。
 */
export function EpisodeMeta({
  episode,
  unitKind,
  inEmptyState = false,
  onSaveMeta,
}: {
  /** 这一集的摘要 */
  episode: Pick<EpisodeSummary, "synopsis" | "hook" | "cliffhanger" | "unitFrom" | "unitTo">;
  /** 单元种类，决定「第 N 章 / 集 / 场 / 段」 */
  unitKind: UnitKind;
  /** 空状态里已经列了梗概、钩子、悬念：页边只留对应原文 */
  inEmptyState?: boolean;
  /** 改钩子 / 悬念；不传就是只读 */
  onSaveMeta?: (field: MetaField, value: string) => Promise<void>;
}) {
  const source = sourceRangeLabel(episode.unitFrom, episode.unitTo, unitKind);
  const meta = inEmptyState ? null : (
    <MetaFields episode={episode} onSave={onSaveMeta} layout="aside" />
  );
  const hasMeta =
    !inEmptyState &&
    (!!onSaveMeta || [episode.synopsis, episode.hook, episode.cliffhanger].some((v) => v.trim()));
  if (!hasMeta && !source) return null;
  return (
    <div data-slot="episode-meta">
      {meta}
      {source && (
        <div className={cn("border-border border-l-2 pl-3", hasMeta && "mt-5")}>
          <b className="text-muted-foreground block text-[12.5px] font-medium">对应原文</b>
          <p className="text-muted-foreground mt-0.5 text-xs tabular-nums">{source}</p>
        </div>
      )}
    </div>
  );
}
