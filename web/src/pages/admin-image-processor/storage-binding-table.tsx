import { CircleCheck, PlugZap } from "lucide-react";

import type { ProcessorPreset, ProcessorView } from "@/api/admin-image-processor/type";
import type { StorageView } from "@/api/admin-storage/type";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  bindingOfStorage,
  enableTarget,
  storageProviderLabel,
} from "@/utils/admin/image-processor";
import { storageLocation } from "@/utils/admin/storage-rules";

/**
 * “存储与处理服务”表：从存储的视角再看一遍，没有处理服务的存储一目了然，并能直接发起启用；
 * 本地磁盘等无法接入的存储，按钮禁用并写明原因。
 * @param storages 存储列表
 * @param processors 处理服务列表
 * @param presets 厂商预设
 * @param canWrite 是否有写权限；没有则不渲染启用按钮
 * @param onEnable 启用：用这套存储和推荐的厂商打开新建抽屉
 */
export function StorageBindingTable({
  storages,
  processors,
  presets,
  canWrite,
  onEnable,
}: {
  storages: StorageView[];
  processors: ProcessorView[];
  presets: ProcessorPreset[];
  canWrite: boolean;
  onEnable: (storage: StorageView, preset: ProcessorPreset) => void;
}) {
  return (
    <div data-slot="storage-binding-table" className="bg-card overflow-hidden rounded-xl border">
      <Table>
        <TableHeader>
          <TableRow className="bg-muted/40 hover:bg-muted/40">
            <TableHead className="px-4">存储</TableHead>
            <TableHead>类型</TableHead>
            <TableHead>处理服务</TableHead>
            {canWrite && <TableHead className="px-4 text-right">操作</TableHead>}
          </TableRow>
        </TableHeader>
        <TableBody>
          {storages.map((storage) => {
            const { published, draft, disabled } = bindingOfStorage(storage.id, processors);
            const target = enableTarget(storage, presets, processors);
            const where = storageLocation(storage);
            return (
              <TableRow key={storage.id} data-storage-id={storage.id} className="align-top">
                <TableCell className="px-4 py-3 whitespace-normal">
                  <div className="flex flex-wrap items-center gap-1.5 font-medium">
                    {storage.name}
                    {storage.is_default && <Tag tone="info">默认</Tag>}
                  </div>
                  <div className="text-muted-foreground font-mono text-[11px]">{where.primary}</div>
                </TableCell>
                <TableCell className="py-3">{storageProviderLabel(storage.provider)}</TableCell>
                <TableCell className="py-3 whitespace-normal">
                  {published ? (
                    <Tag tone="success">
                      <CircleCheck />
                      {published.name}
                    </Tag>
                  ) : draft ? (
                    <Tag>草稿：{draft.name}</Tag>
                  ) : disabled ? (
                    <Tag tone="warning">已停用：{disabled.name}</Tag>
                  ) : (
                    <span className="text-muted-foreground">未启用 · 回退原图 / 占位</span>
                  )}
                </TableCell>
                {canWrite && (
                  <TableCell className="px-4 py-3 text-right">
                    {!published && !draft && (
                      <ReasonTooltip reason={target.reason}>
                        <Button
                          variant="outline"
                          size="xs"
                          disabled={!target.preset}
                          aria-label={`为 ${storage.name} 启用处理服务`}
                          onClick={() => target.preset && onEnable(storage, target.preset)}
                        >
                          <PlugZap />
                          启用处理服务
                        </Button>
                      </ReasonTooltip>
                    )}
                  </TableCell>
                )}
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
    </div>
  );
}
