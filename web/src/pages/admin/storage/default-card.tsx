import type { StoragePreset, StorageView } from "@/api/admin-storage/type.d";
import {
  DescriptionDetails,
  DescriptionItem,
  DescriptionList,
  DescriptionTerm,
} from "@/components/admin-ui/description-list";
import { Notice } from "@/components/admin-ui/notice";
import { Tag } from "@/components/admin-ui/tag";
import { Card, CardContent } from "@/components/ui/card";
import { formatCount, providerLabel, storageLocation } from "@/utils/admin/storage-rules";

import { PROVIDER_META } from "./provider-meta";
import { StorageStatus } from "./storage-status";

/**
 * 默认存储卡片：先回答“现在新文件写到哪”这一个问题，只读角色也能一眼确认。
 * @param storage 当前默认存储
 * @param presets 服务商预设（取服务商显示名）
 */
export function DefaultStorageCard({
  storage,
  presets,
}: {
  storage: StorageView;
  presets: StoragePreset[];
}) {
  const Icon = PROVIDER_META[storage.provider].icon;
  const where = storageLocation(storage);
  return (
    <Card data-slot="default-storage-card">
      <CardContent className="flex flex-wrap items-center gap-x-6 gap-y-4">
        <span className="bg-muted text-muted-foreground grid size-10 shrink-0 place-items-center rounded-lg">
          <Icon className="size-5" />
        </span>
        <div className="min-w-0">
          <div className="text-muted-foreground text-xs">
            默认存储 · 新上传和新生成的素材写到这里
          </div>
          <div className="mt-0.5 flex flex-wrap items-center gap-2 font-medium">
            {storage.name}
            <Tag>{providerLabel(storage.provider, presets)}</Tag>
            {storage.builtin && <Tag>内置</Tag>}
          </div>
        </div>
        <DescriptionList className="ml-auto grid-cols-3 gap-x-8 md:grid-cols-3 2xl:grid-cols-3">
          <DescriptionItem>
            <DescriptionTerm>位置</DescriptionTerm>
            <DescriptionDetails className="flex-col items-start gap-0">
              <span className="font-mono text-xs">{where.primary}</span>
              {where.secondary && (
                <span className="text-muted-foreground text-[11px] font-normal">
                  {where.secondary}
                </span>
              )}
            </DescriptionDetails>
          </DescriptionItem>
          <DescriptionItem>
            <DescriptionTerm>素材数</DescriptionTerm>
            <DescriptionDetails className="font-mono tabular-nums">
              {formatCount(storage.asset_count)}
            </DescriptionDetails>
          </DescriptionItem>
          <DescriptionItem>
            <DescriptionTerm>连通</DescriptionTerm>
            <DescriptionDetails>
              <StorageStatus storage={storage} />
            </DescriptionDetails>
          </DescriptionItem>
        </DescriptionList>
      </CardContent>
    </Card>
  );
}

/** 默认存储是本地磁盘时的橙色提示条：本地地址上游 AI 平台访问不到，生产建议对象存储 */
export function LocalDefaultNotice() {
  return (
    <Notice tone="warning" title="当前默认是本地磁盘。">
      本地存储的地址上游 AI 平台访问不到（参考图以 URL
      传给模型会失败），也不便于多实例部署。生产环境建议新建对象存储并设为默认。
    </Notice>
  );
}
