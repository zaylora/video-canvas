import { useMemo, useState } from "react";
import { AudioLines, Image as ImageIcon, Plus, Type, Video, type LucideIcon } from "lucide-react";

import type { ChannelView, ConfigListItem } from "@/api/admin-ai/type";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { MODEL_KIND_LABEL, MODEL_KINDS } from "@/utils/admin/model-body";

import { NativeSelect, Tag } from "../shared";

/** kind 的图标（形状加文字标签，不只靠颜色） */
const KIND_ICON: Record<string, LucideIcon> = {
  text: Type,
  video: Video,
  image: ImageIcon,
  audio: AudioLines,
};

/** 列表项的发布状态 */
const statusOf = (item: ConfigListItem): "published" | "draft" | "unpublished" =>
  item.has_unpublished_draft
    ? "draft"
    : item.published_revision_no != null
      ? "published"
      : "unpublished";

/**
 * 模型页左栏：模型列表 + kind 与状态筛选。
 * 行内：展示名（没有则 key）、kind 图标、发布状态、上架状态；只有后端补了 channel 字段才显示渠道名，
 * 补之前不显示这一列，也不为它逐个请求详情。
 */
export function ModelList({
  models,
  status,
  selectedKey,
  isNew,
  channels,
  onSelect,
  onNew,
  onRetry,
}: {
  models: ConfigListItem[];
  status: "loading" | "ready" | "error";
  selectedKey: string | null;
  isNew: boolean;
  channels: ChannelView[];
  onSelect: (key: string) => void;
  onNew: () => void;
  onRetry: () => void;
}) {
  const [kind, setKind] = useState("");
  const [state, setState] = useState("");
  const shown = useMemo(
    () =>
      models.filter(
        (item) => (!kind || item.kind === kind) && (!state || statusOf(item) === state),
      ),
    [models, kind, state],
  );

  return (
    <nav
      aria-label="模型列表"
      className="flex max-h-64 w-full shrink-0 flex-col border-b lg:max-h-none lg:w-64 lg:border-r lg:border-b-0"
    >
      <div className="flex items-center gap-2 border-b p-2">
        <span className="text-sm font-medium">模型（{models.length}）</span>
        <Button
          size="icon-sm"
          variant="outline"
          className="ml-auto"
          aria-label="新建模型"
          onClick={onNew}
        >
          <Plus />
        </Button>
      </div>
      <div className="grid grid-cols-2 gap-1.5 border-b p-2">
        <NativeSelect
          aria-label="按 kind 筛选"
          className="h-7 text-xs"
          value={kind}
          onChange={(event) => setKind(event.target.value)}
        >
          <option value="">全部 kind</option>
          {MODEL_KINDS.map((item) => (
            <option key={item} value={item}>
              {MODEL_KIND_LABEL[item]}
            </option>
          ))}
        </NativeSelect>
        <NativeSelect
          aria-label="按状态筛选"
          className="h-7 text-xs"
          value={state}
          onChange={(event) => setState(event.target.value)}
        >
          <option value="">全部状态</option>
          <option value="published">已发布</option>
          <option value="draft">有未发布草稿</option>
          <option value="unpublished">未上架</option>
        </NativeSelect>
      </div>
      <ul className="min-h-0 flex-1 overflow-y-auto p-1.5">
        {isNew && (
          <li
            className="text-primary bg-primary/10 rounded-lg px-2.5 py-2 text-xs"
            aria-current="true"
          >
            新建模型（未保存）
          </li>
        )}
        {status === "loading" &&
          Array.from({ length: 3 }, (_, index) => (
            <li key={index} className="p-2">
              <Skeleton className="h-4 w-2/3" />
              <Skeleton className="mt-1.5 h-3 w-full" />
            </li>
          ))}
        {status === "error" && (
          <li className="text-destructive p-3 text-xs">
            加载失败，
            <button type="button" className="underline" onClick={onRetry}>
              重试
            </button>
          </li>
        )}
        {status === "ready" && models.length === 0 && !isNew && (
          <li className="text-muted-foreground p-3 text-xs">还没有模型，点右上角 + 新建。</li>
        )}
        {status === "ready" && models.length > 0 && shown.length === 0 && (
          <li className="text-muted-foreground p-3 text-xs">没有符合筛选条件的模型</li>
        )}
        {shown.map((item) => {
          const active = !isNew && selectedKey === item.key;
          const Icon = item.kind ? (KIND_ICON[item.kind] ?? Type) : Type;
          const channel = item.channel
            ? (channels.find((c) => c.key === item.channel)?.name ?? item.channel)
            : null;
          const publishState = statusOf(item);
          return (
            <li key={item.key}>
              <button
                type="button"
                aria-current={active}
                className={cn(
                  "hover:bg-muted flex w-full flex-col gap-1 rounded-lg px-2.5 py-2 text-left",
                  active && "bg-muted",
                )}
                onClick={() => onSelect(item.key)}
              >
                <span className="flex items-center gap-1.5">
                  <Icon className="text-muted-foreground size-3.5 shrink-0" aria-hidden />
                  <span className="truncate text-sm">{item.label || item.name || item.key}</span>
                </span>
                <span className="text-muted-foreground truncate font-mono text-[11px]">
                  {item.key}
                </span>
                <span className="flex flex-wrap items-center gap-1 text-[11px]">
                  {item.kind && <Tag>{MODEL_KIND_LABEL[item.kind] ?? item.kind}</Tag>}
                  {item.published_revision_no != null ? (
                    <Tag tone="info">已发布 v{item.published_revision_no}</Tag>
                  ) : (
                    <Tag>未发布</Tag>
                  )}
                  {publishState === "draft" && <Tag tone="warning">有未发布草稿</Tag>}
                  <Tag tone={item.enabled ? "success" : "neutral"}>
                    {item.enabled ? "已上架" : "未上架"}
                  </Tag>
                </span>
                {channel && (
                  <span className="text-muted-foreground truncate text-[11px]">
                    渠道：{channel}
                  </span>
                )}
              </button>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
