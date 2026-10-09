import { Plus } from "lucide-react";
import { useCallback, useRef, useState } from "react";

import type { ProcessorView } from "@/api/admin/image-processor/type.d";
import {
  EmptyState,
  EmptyStateActions,
  EmptyStateDescription,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import {
  PageHeader,
  PageHeaderActions,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminStore } from "@/store/admin";
import { canManageInfra } from "@/utils/admin/role";

import { ReadOnlyNotice } from "../shared";
import { confirmDelete, confirmDisable, confirmRollback } from "./processor-actions";
import { ProcessorDialog, type ProcessorDialogTarget } from "./processor-dialog";
import { ProcessorTable } from "./processor-table";
import { StorageBindingTable } from "./storage-binding-table";
import { useImageProcessors } from "./use-image-processors";

/**
 * 图片服务管理页：处理服务列表 + “存储与处理服务”表 + 弹窗（四步向导）。
 * 写操作（新建、编辑、校验、发布、回滚、停用、删除）只对 super_admin 渲染，admin 只读；
 * 请求错误的全局提示由拦截器弹，这里不重复。
 */
export default function ImageProcessorPage() {
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const { processors, presets, storages, status, reload, upsertOne } = useImageProcessors();
  const [target, setTarget] = useState<ProcessorDialogTarget | null>(null);
  /** 每次打开弹窗递增，让向导重新初始化 */
  const nonceRef = useRef(0);

  const open = useCallback(
    (next: DistributiveOmit<ProcessorDialogTarget, "nonce">) =>
      setTarget({ ...next, nonce: ++nonceRef.current }),
    [],
  );
  const refresh = useCallback(() => void reload(), [reload]);

  const actions = {
    onOpen: (processor: ProcessorView) => open({ kind: "edit", id: processor.id }),
    onVerify: (processor: ProcessorView) => open({ kind: "edit", id: processor.id, step: 4 }),
    onRollback: (processor: ProcessorView) => void confirmRollback(processor, refresh),
    onDisable: (processor: ProcessorView) => void confirmDisable(processor, refresh),
    onDelete: (processor: ProcessorView) => void confirmDelete(processor, refresh),
  };

  return (
    <div className="h-full overflow-y-auto">
      <main className="px-4 py-6 lg:px-6">
        <PageHeader>
          <PageHeaderHeading>
            <PageHeaderTitle>图片服务</PageHeaderTitle>
          </PageHeaderHeading>
          {canWrite && (
            <PageHeaderActions>
              <Button disabled={status !== "ready"} onClick={() => open({ kind: "new" })}>
                <Plus />
                新建处理服务
              </Button>
            </PageHeaderActions>
          )}
        </PageHeader>

        {!canWrite && (
          <ReadOnlyNotice
            className="mb-4"
            what="新建、编辑、校验、发布、回滚、停用、删除处理服务"
          />
        )}

        {status === "loading" && (
          <div className="flex flex-col gap-4" aria-busy="true">
            <Skeleton className="h-40" />
            <Skeleton className="h-48" />
          </div>
        )}

        {status === "error" && (
          <EmptyState>
            <EmptyStateTitle className="text-destructive">加载失败</EmptyStateTitle>
            <EmptyStateActions>
              <Button variant="outline" size="sm" onClick={() => void reload()}>
                重试
              </Button>
            </EmptyStateActions>
          </EmptyState>
        )}

        {status === "ready" && (
          <div className="flex flex-col gap-6">
            <section aria-labelledby="processor-list-title" className="flex flex-col gap-2">
              <h2 id="processor-list-title" className="text-sm font-semibold">
                处理服务
              </h2>
              {processors.length > 0 ? (
                <ProcessorTable
                  processors={processors}
                  presets={presets}
                  canWrite={canWrite}
                  actions={actions}
                />
              ) : (
                <EmptyState>
                  <EmptyStateTitle>还没有处理服务</EmptyStateTitle>
                  <EmptyStateDescription>所有存储都回退原图 / 占位。</EmptyStateDescription>
                </EmptyState>
              )}
            </section>

            <section aria-labelledby="storage-binding-title" className="flex flex-col gap-2">
              <h2 id="storage-binding-title" className="text-sm font-semibold">
                存储与处理服务
              </h2>
              <StorageBindingTable
                storages={storages}
                processors={processors}
                presets={presets}
                canWrite={canWrite}
                onEnable={(storage) => open({ kind: "new", storageId: storage.id })}
              />
            </section>
          </div>
        )}

        <ProcessorDialog
          target={target}
          processors={processors}
          presets={presets}
          storages={storages}
          canWrite={canWrite}
          onChanged={upsertOne}
          onPublished={() => {
            setTarget(null);
            refresh();
          }}
          onClose={() => {
            setTarget(null);
            refresh();
          }}
        />
      </main>
    </div>
  );
}

/** 对联合类型的每个分支分别去掉字段（Omit 会把联合压扁） */
type DistributiveOmit<T, K extends PropertyKey> = T extends unknown ? Omit<T, K> : never;
