import { Plus } from "lucide-react";
import { useCallback, useState } from "react";
import { toast } from "sonner";

import { checkStorage } from "@/api/admin/storage";
import type { StorageView } from "@/api/admin/storage/type.d";
import { AdminMain } from "@/components/admin-ui/admin-main";
import { EmptyState, EmptyStateActions, EmptyStateTitle } from "@/components/admin-ui/empty-state";
import {
  PageHeader,
  PageHeaderActions,
  PageHeaderDescription,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useAdminStore } from "@/store/admin";
import { canManageInfra } from "@/utils/admin/role";
import { probeSummary } from "@/utils/admin/storage-probe";

import { ReadOnlyNotice } from "../shared";
import { DefaultStorageCard, LocalDefaultNotice } from "./default-card";
import { confirmSetDefault, requestDeleteStorage } from "./storage-dialogs";
import { StorageSheet, type StorageSheetTarget } from "./storage-sheet";
import { StorageTable } from "./storage-table";
import { useStorages } from "./use-storages";

/**
 * 存储配置页：默认存储卡片 + 存储列表 + 右侧抽屉（新建 / 编辑）。
 * 写操作（新建、编辑、测试、设为默认、删除）只对 super_admin 渲染，admin 只读；
 * 请求错误的全局提示由拦截器弹，这里不重复。
 */
export default function StoragePage() {
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const { storages, presets, status, reload, replaceOne } = useStorages();
  const [target, setTarget] = useState<StorageSheetTarget | null>(null);
  /** 正在测试或做删除预检的存储，对应行的按钮转圈并禁用 */
  const [busyId, setBusyId] = useState<number | null>(null);

  const current = storages.find((item) => item.is_default);

  const check = useCallback(
    async (storage: StorageView) => {
      setBusyId(storage.id);
      try {
        const result = await checkStorage(storage.id);
        const summary = probeSummary(result);
        if (result.ok) toast.success(`「${storage.name}」测试通过`);
        else toast.error(`「${storage.name}」测试未通过：${summary.text}`);
        await reload();
      } catch {
        // 请求失败的全局 toast 已弹，行上的按钮在 finally 里恢复
      } finally {
        setBusyId(null);
      }
    },
    [reload],
  );

  const remove = useCallback(
    async (storage: StorageView) => {
      setBusyId(storage.id);
      try {
        await requestDeleteStorage(storage, () => void reload());
      } catch {
        // 删除预检失败的全局 toast 已弹
      } finally {
        setBusyId(null);
      }
    },
    [reload],
  );

  return (
    <div className="h-full overflow-y-auto">
      <AdminMain>
        <PageHeader>
          <PageHeaderHeading>
            <PageHeaderTitle>存储配置</PageHeaderTitle>
            <PageHeaderDescription>
              素材（上传与生成产物）存到哪里。切换默认只影响新文件，已有素材始终从它所在的存储读取。
            </PageHeaderDescription>
          </PageHeaderHeading>
          {canWrite && (
            <PageHeaderActions>
              <Button disabled={status !== "ready"} onClick={() => setTarget({ kind: "new" })}>
                <Plus />
                新建存储
              </Button>
            </PageHeaderActions>
          )}
        </PageHeader>

        {!canWrite && (
          <ReadOnlyNotice className="mb-4" what="新建、编辑、测试、设为默认、删除存储" />
        )}

        {status === "loading" && (
          <div className="flex flex-col gap-4" aria-busy="true">
            <Skeleton className="h-24" />
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
          <div className="flex flex-col gap-4">
            {current?.provider === "local" && <LocalDefaultNotice />}
            {current && <DefaultStorageCard storage={current} presets={presets} />}
            <StorageTable
              storages={storages}
              presets={presets}
              canWrite={canWrite}
              busyId={busyId}
              actions={{
                onOpen: (storage) => setTarget({ kind: "edit", id: storage.id }),
                onCheck: (storage) => void check(storage),
                onSetDefault: (storage) =>
                  void confirmSetDefault(storage, current, () => void reload()),
                onDelete: (storage) => void remove(storage),
              }}
            />
          </div>
        )}

        <StorageSheet
          target={target}
          storages={storages}
          presets={presets}
          canWrite={canWrite}
          onSaved={() => {
            setTarget(null);
            void reload();
          }}
          onChanged={replaceOne}
          onClose={() => setTarget(null)}
        />
      </AdminMain>
    </div>
  );
}
