import { Check, Pencil, Trash2, X } from "lucide-react";
import { useState } from "react";

import { renameSkill } from "@/api/admin/agent-skill";
import type { SkillItem, SkillVersionView } from "@/api/admin/agent-skill/type.d";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  deleteBlockReason,
  formatDateTime,
  formatSize,
  toggleBlockReason,
  versionBadge,
} from "@/utils/admin/agent-skill";

import { FrontmatterCard } from "./file-preview";

/** 一个带标题的区块 */
function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section>
      <h3 className="mb-2 text-sm font-medium">{title}</h3>
      {children}
    </section>
  );
}

/** 显示名：只读文本 + “修改”，点开变成输入框；Enter 保存、Esc 取消。内置技能只读 */
function TitleField({
  item,
  onRenamed,
}: {
  item: SkillItem;
  onRenamed: (item: SkillItem) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(item.title);
  const [saving, setSaving] = useState(false);
  const trimmed = draft.trim();

  const save = async () => {
    if (!trimmed || trimmed === item.title) {
      setEditing(false);
      return;
    }
    setSaving(true);
    try {
      onRenamed(await renameSkill(item.name, trimmed));
      setEditing(false);
    } catch {
      // 失败的全局 toast 已弹，保持编辑状态方便改
    } finally {
      setSaving(false);
    }
  };

  if (!editing) {
    return (
      <span className="flex items-center gap-1.5">
        {item.title}
        {!item.readonly && (
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label="修改显示名"
            onClick={() => {
              setDraft(item.title);
              setEditing(true);
            }}
          >
            <Pencil />
          </Button>
        )}
      </span>
    );
  }
  return (
    <span className="flex items-center gap-1.5">
      <Input
        autoFocus
        value={draft}
        maxLength={64}
        aria-label="显示名"
        disabled={saving}
        className="h-8 max-w-64"
        onChange={(event) => setDraft(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === "Enter") void save();
          if (event.key === "Escape") {
            event.stopPropagation();
            setEditing(false);
          }
        }}
      />
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label="保存"
        disabled={saving || !trimmed}
        onClick={() => void save()}
      >
        <Check />
      </Button>
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label="取消修改"
        disabled={saving}
        onClick={() => setEditing(false)}
      >
        <X />
      </Button>
    </span>
  );
}

/**
 * 详情弹窗的“概览”：状态卡（开关 + 一句话）、生效版本卡、基本信息（显示名可改）、头部字段（已采用 / 未支持）、危险操作。
 * @param item 技能（列表里的最新视图）
 * @param version 生效版本的完整信息（取头部字段）；内置技能或还没加载完为 null
 * @param busy 正在启停：开关禁用
 * @param onToggle 启停
 * @param onGoVersions 跳到版本页签（“去切换”）
 * @param onRenamed 显示名改好后
 * @param onDelete 点“删除技能”
 */
export function SkillOverview({
  item,
  version,
  busy,
  onToggle,
  onGoVersions,
  onRenamed,
  onDelete,
}: {
  item: SkillItem;
  version: SkillVersionView | null;
  busy: boolean;
  onToggle: (enabled: boolean) => void;
  onGoVersions: () => void;
  onRenamed: (item: SkillItem) => void;
  onDelete: () => void;
}) {
  const badge = versionBadge(item);
  const toggleReason = toggleBlockReason(item);
  const deleteReason = deleteBlockReason(item);
  const unsupported = version?.unsupported_fields ?? item.unsupported_fields ?? [];
  const frontmatter =
    version?.frontmatter && Object.keys(version.frontmatter).length > 0
      ? version.frontmatter
      : { name: item.name, description: item.description };

  return (
    <div className="grid gap-5">
      <div className="grid grid-cols-2 gap-3 max-md:grid-cols-1">
        <div className="rounded-xl border p-4">
          <p className="text-muted-foreground text-xs">状态</p>
          <div className="mt-2 flex items-center gap-3">
            <ReasonTooltip reason={toggleReason}>
              <Switch
                checked={item.enabled}
                disabled={!!toggleReason || busy}
                aria-label={`${item.enabled ? "停用" : "启用"}技能 ${item.title}`}
                onCheckedChange={onToggle}
              />
            </ReasonTooltip>
            <span className="text-sm font-medium">{item.enabled ? "已启用" : "已停用"}</span>
          </div>
          <p className="text-muted-foreground mt-2 text-sm leading-relaxed">
            {item.readonly
              ? "内置技能随版本发布，一直可用，不能停用。"
              : item.enabled
                ? "新开始的 Agent 运行会用到它，进行中的运行不受影响。"
                : "Agent 不会使用它，@ 弹层里也看不到。"}
          </p>
        </div>

        <div className="rounded-xl border p-4">
          <p className="text-muted-foreground text-xs">生效版本</p>
          <p className="mt-1.5 font-mono text-3xl font-semibold tracking-tight tabular-nums">
            {badge.active}
          </p>
          {item.source === "builtin" ? (
            <p className="text-muted-foreground mt-2 text-sm">内置技能没有版本历史。</p>
          ) : (
            <div className="mt-2 flex flex-wrap items-center gap-2">
              <span className="text-muted-foreground text-sm tabular-nums">
                共 {item.version_count} 个版本
              </span>
              {badge.pending && (
                <>
                  <Tag tone="warning" className="tabular-nums">
                    {badge.pending}
                  </Tag>
                  <Button variant="link" size="xs" onClick={onGoVersions}>
                    去切换
                  </Button>
                </>
              )}
            </div>
          )}
        </div>
      </div>

      <Section title="基本信息">
        <dl className="grid grid-cols-[88px_minmax(0,1fr)] gap-x-3 gap-y-2.5 text-sm">
          <dt className="text-muted-foreground">显示名</dt>
          <dd className="min-w-0">
            <TitleField item={item} onRenamed={onRenamed} />
          </dd>
          <dt className="text-muted-foreground">技能名</dt>
          <dd className="font-mono text-sm">{item.name}</dd>
          <dt className="text-muted-foreground">说明</dt>
          <dd className="min-w-0 break-words">{item.description}</dd>
          <dt className="text-muted-foreground">来源</dt>
          <dd>
            <Tag tone={item.source === "builtin" ? "neutral" : "violet"}>
              {item.source === "builtin" ? "内置" : "导入"}
            </Tag>
          </dd>
          <dt className="text-muted-foreground">内容</dt>
          <dd className="tabular-nums">
            {item.file_count} 个文件 · {formatSize(item.total_bytes)}
          </dd>
          <dt className="text-muted-foreground">更新时间</dt>
          <dd className="tabular-nums">{formatDateTime(item.updated_at)}</dd>
        </dl>
      </Section>

      <Section title="头部字段">
        <FrontmatterCard className="m-0" frontmatter={frontmatter} unsupported={unsupported} />
      </Section>

      {!item.readonly && (
        <Section title="危险操作">
          <div className="border-destructive/30 flex flex-wrap items-center gap-3 rounded-xl border p-4">
            <div className="min-w-0 flex-1 text-sm">
              <p className="font-medium">删除技能</p>
              <p className="text-muted-foreground mt-0.5">
                {deleteReason ?? "删除全部版本和整包，不能撤销。历史会话不受影响。"}
              </p>
            </div>
            <ReasonTooltip reason={deleteReason}>
              <Button variant="destructive" disabled={!!deleteReason} onClick={onDelete}>
                <Trash2 />
                删除技能
              </Button>
            </ReasonTooltip>
          </div>
        </Section>
      )}
    </div>
  );
}
