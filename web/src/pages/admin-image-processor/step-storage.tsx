import { Lock, TriangleAlert } from "lucide-react";
import { Link } from "react-router";

import type { ProcessorPreset, ProcessorView } from "@/api/admin-image-processor/type";
import type { StorageView } from "@/api/admin-storage/type";
import { ChoiceCard, ChoiceCardGroup } from "@/components/admin-ui/choice-card";
import {
  EmptyState,
  EmptyStateDescription,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import { Notice } from "@/components/admin-ui/notice";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { evaluateStorage, storageProviderLabel } from "@/utils/admin/image-processor";
import { storageLocation } from "@/utils/admin/storage-rules";

/**
 * 第二步：绑定存储。与厂商不匹配的存储灰掉并写明原因；被另一个已发布的处理服务占用只是提示（发布会替换它）。
 * 没有任何可绑定的存储时给空状态，并链接到存储配置去新建。
 * @param preset 第一步选的厂商预设
 * @param storages 全部存储
 * @param processors 全部处理服务（判定占用）
 * @param value 当前选中的存储 ID
 * @param selfId 正在编辑的处理服务 ID（占用判定排除自己）
 * @param disabled 整组禁用（只读，或已创建的处理服务不能换存储）
 * @param onChange 选择存储
 */
export function StepStorage({
  preset,
  storages,
  processors,
  value,
  selfId,
  disabled,
  onChange,
}: {
  preset: ProcessorPreset;
  storages: StorageView[];
  processors: ProcessorView[];
  value: number | null;
  selfId: number | null;
  disabled: boolean;
  onChange: (storage: StorageView) => void;
}) {
  const items = storages.map((storage) => ({
    storage,
    choice: evaluateStorage(preset, storage, processors, selfId),
  }));
  const hasSelectable = items.some((item) => item.choice.selectable);
  const providerName = storageProviderLabel(preset.storage_provider);

  return (
    <div className="flex flex-col gap-3">
      <Notice tone="info" title={preset.name}>
        只能绑定「{providerName}」存储。一个处理服务只绑定一个存储，创建后不能更改。
      </Notice>

      {!hasSelectable && (
        <EmptyState>
          <EmptyStateTitle>没有可绑定的「{providerName}」存储</EmptyStateTitle>
          <EmptyStateDescription>
            请先到存储配置新建一套{providerName}
            {preset.requires_public_base && "并设置公开域名"}，再回来绑定。
          </EmptyStateDescription>
          <Button variant="outline" size="sm" render={<Link to="/admin/settings/storage" />}>
            去存储配置新建
          </Button>
        </EmptyState>
      )}

      <ChoiceCardGroup aria-label="绑定到哪套存储">
        {items.map(({ storage, choice }) => {
          const where = storageLocation(storage);
          return (
            <ChoiceCard
              key={storage.id}
              indicator
              selected={storage.id === value}
              disabled={disabled || !choice.selectable}
              onClick={() => onChange(storage)}
            >
              <span className="flex min-w-0 flex-1 flex-col gap-1">
                <span className="flex flex-wrap items-center gap-1.5 text-sm font-medium">
                  {storage.name}
                  <Tag>{storageProviderLabel(storage.provider)}</Tag>
                </span>
                {choice.reason ? (
                  <span className="flex items-start gap-1 text-xs text-amber-700 dark:text-amber-400">
                    <Lock className="mt-0.5 size-3 shrink-0" />
                    {choice.reason}
                  </span>
                ) : (
                  <span className="text-muted-foreground font-mono text-[11px]">
                    {where.primary}
                    {where.secondary && ` · ${where.secondary}`}
                  </span>
                )}
                {choice.note && (
                  <span className="flex items-start gap-1 text-xs text-amber-700 dark:text-amber-400">
                    <TriangleAlert className="mt-0.5 size-3 shrink-0" />
                    {choice.note}
                  </span>
                )}
              </span>
            </ChoiceCard>
          );
        })}
      </ChoiceCardGroup>
    </div>
  );
}
