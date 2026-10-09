import { Lightbulb, Upload } from "lucide-react";
import { useEffect, useState, type KeyboardEvent } from "react";

import type { SkillItem } from "@/api/admin/agent-skill/type.d";
import {
  EmptyState,
  EmptyStateActions,
  EmptyStateDescription,
  EmptyStateIcon,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useIsMobile } from "@/hooks/use-mobile";
import { SKELETON_DELAY } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { toggleBlockReason, versionBadge } from "@/utils/admin/agent-skill";

/** loading 持续超过 delay 毫秒才返回 true：缓存命中很快返回时不闪灰块 */
function useDelayedFlag(flag: boolean, delay: number) {
  const [shown, setShown] = useState(false);
  useEffect(() => {
    if (!flag) {
      setShown(false);
      return;
    }
    const timer = setTimeout(() => setShown(true), delay);
    return () => clearTimeout(timer);
  }, [flag, delay]);
  return shown;
}

/** 新导入的行：一层铺满整行的浅紫，从 1 淡到 0（只动 opacity），播完停在透明 */
const FRESH_ROW =
  "relative after:pointer-events-none after:absolute after:inset-0 after:bg-violet-500/14 after:content-[''] after:animate-out after:fade-out-0 after:duration-240 after:fill-mode-forwards motion-reduce:after:duration-150";

/** 启停开关：内置 / 没有生效版本时禁用并悬停写原因；点它不触发整行的“打开详情” */
function SkillSwitch({
  item,
  busy,
  onToggle,
}: {
  item: SkillItem;
  busy: boolean;
  onToggle: (item: SkillItem, enabled: boolean) => void;
}) {
  const reason = toggleBlockReason(item);
  return (
    <span
      data-slot="skill-switch"
      className="inline-flex"
      onClick={(event) => event.stopPropagation()}
      onKeyDown={(event) => event.stopPropagation()}
    >
      <ReasonTooltip reason={reason}>
        <Switch
          checked={item.enabled}
          disabled={!!reason || busy}
          aria-label={`${item.enabled ? "停用" : "启用"}技能 ${item.title}`}
          onCheckedChange={(next) => onToggle(item, next)}
        />
      </ReasonTooltip>
    </span>
  );
}

/** 名称单元：图标块 + 显示名 + name 等宽小字 */
function NameCell({ item }: { item: SkillItem }) {
  return (
    <div className="flex min-w-0 items-center gap-2.5">
      <span className="bg-muted text-muted-foreground grid size-8 shrink-0 place-items-center rounded-lg border">
        <Lightbulb className="size-4" />
      </span>
      <span className="grid min-w-0">
        <span className="truncate font-medium">{item.title}</span>
        <span className="text-muted-foreground truncate font-mono text-xs">{item.name}</span>
      </span>
    </div>
  );
}

/** 来源与内容：内置 / 导入 + 文件数 */
function SourceTags({ item }: { item: SkillItem }) {
  return (
    <div className="flex flex-wrap gap-1">
      <Tag tone={item.source === "builtin" ? "neutral" : "violet"}>
        {item.source === "builtin" ? "内置" : "导入"}
      </Tag>
      <Tag className="tabular-nums">{item.file_count} 个文件</Tag>
    </div>
  );
}

/** 生效版本：v2；有更新版本未生效时右侧琥珀“v3 待生效” */
function VersionCell({ item }: { item: SkillItem }) {
  const badge = versionBadge(item);
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="font-mono text-sm tabular-nums">{badge.active}</span>
      {badge.pending && (
        <Tag tone="warning" className="tabular-nums">
          {badge.pending}
        </Tag>
      )}
    </div>
  );
}

/** 行上 Enter 打开详情；开关等内部控件自己的按键不触发 */
const openOnEnter = (event: KeyboardEvent<HTMLElement>, open: () => void) => {
  if (event.key === "Enter" && event.target === event.currentTarget) open();
};

/**
 * 技能列表：≥ 768 px 用表格，窄屏变卡片（名称 + 开关 / 说明 / 标签 + 版本）。
 * 点行或在行上按 Enter 打开详情；加载超过 150 ms 才显示骨架行；筛选无结果与“还没有导入过技能”各有说明。
 * @param items 已过滤的行
 * @param loading 首次加载中
 * @param hasImported 全部技能里有没有导入过的（没有则在表下方提示去导入）
 * @param busyNames 正在启停的技能名
 * @param freshName 刚导入的技能名，对应行一次性高亮
 * @param onOpen 打开详情
 * @param onToggle 启停
 * @param onImport 打开导入对话框
 */
export function SkillTable({
  items,
  loading,
  hasImported,
  busyNames,
  freshName,
  onOpen,
  onToggle,
  onImport,
}: {
  items: SkillItem[];
  loading: boolean;
  hasImported: boolean;
  busyNames: ReadonlySet<string>;
  freshName: string | null;
  onOpen: (item: SkillItem) => void;
  onToggle: (item: SkillItem, enabled: boolean) => void;
  onImport: () => void;
}) {
  const mobile = useIsMobile();
  const showSkeleton = useDelayedFlag(loading, SKELETON_DELAY * 1000);

  if (loading) {
    return (
      <div
        data-slot="skill-table"
        aria-busy="true"
        className="bg-card flex flex-col overflow-hidden rounded-xl border"
      >
        {showSkeleton &&
          [0, 1, 2, 3].map((i) => (
            <div key={i} className="flex min-h-15 items-center gap-3 border-b px-4 last:border-b-0">
              <Skeleton className="size-8 rounded-lg" />
              <Skeleton className="h-4 w-40" />
              <Skeleton className="ml-auto h-4 w-24" />
            </div>
          ))}
        {!showSkeleton && <div className="min-h-60" />}
      </div>
    );
  }

  return (
    <div data-slot="skill-table" className="flex flex-col gap-4">
      {items.length === 0 ? (
        <EmptyState>
          <EmptyStateTitle>没有符合条件的技能</EmptyStateTitle>
        </EmptyState>
      ) : mobile ? (
        <ul className="flex flex-col gap-2.5">
          {items.map((item) => (
            <li
              key={item.name}
              role="button"
              tabIndex={0}
              onClick={() => onOpen(item)}
              onKeyDown={(event) => openOnEnter(event, () => onOpen(item))}
              className={cn(
                "bg-card hover:bg-muted/50 focus-visible:ring-ring/50 grid gap-2 rounded-xl border p-3.5 transition-colors outline-none focus-visible:ring-2",
                item.name === freshName && FRESH_ROW,
              )}
            >
              <div className="flex items-center justify-between gap-3">
                <NameCell item={item} />
                <SkillSwitch item={item} busy={busyNames.has(item.name)} onToggle={onToggle} />
              </div>
              <p className="text-muted-foreground line-clamp-2 text-sm">{item.description}</p>
              <div className="flex items-center justify-between gap-2">
                <SourceTags item={item} />
                <VersionCell item={item} />
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <div className="bg-card overflow-hidden rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/40 hover:bg-muted/40">
                <TableHead className="w-[28%] px-4">名称</TableHead>
                <TableHead>说明</TableHead>
                <TableHead className="w-48">来源与内容</TableHead>
                <TableHead className="w-36">生效版本</TableHead>
                <TableHead className="w-16 px-4 text-right">启用</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((item) => (
                <TableRow
                  key={item.name}
                  data-skill-name={item.name}
                  tabIndex={0}
                  onClick={() => onOpen(item)}
                  onKeyDown={(event) => openOnEnter(event, () => onOpen(item))}
                  className={cn(
                    "focus-visible:ring-ring/50 min-h-15 cursor-pointer outline-none focus-visible:ring-2 focus-visible:ring-inset",
                    item.name === freshName && FRESH_ROW,
                  )}
                >
                  <TableCell className="px-4 py-3">
                    <NameCell item={item} />
                  </TableCell>
                  <TableCell className="text-muted-foreground max-w-0 py-3 whitespace-normal">
                    <span className="line-clamp-2">{item.description}</span>
                  </TableCell>
                  <TableCell className="py-3">
                    <SourceTags item={item} />
                  </TableCell>
                  <TableCell className="py-3">
                    <VersionCell item={item} />
                  </TableCell>
                  <TableCell className="px-4 py-3 text-right">
                    <SkillSwitch item={item} busy={busyNames.has(item.name)} onToggle={onToggle} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      {!hasImported && (
        <EmptyState>
          <EmptyStateIcon>
            <Upload />
          </EmptyStateIcon>
          <EmptyStateTitle>还没有导入过技能</EmptyStateTitle>
          <EmptyStateDescription>支持 zip、文件夹或单个 SKILL.md</EmptyStateDescription>
          <EmptyStateActions>
            <Button onClick={onImport}>
              <Upload />
              导入技能
            </Button>
          </EmptyStateActions>
        </EmptyState>
      )}
    </div>
  );
}
