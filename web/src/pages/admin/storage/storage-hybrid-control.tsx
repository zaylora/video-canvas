import { Loader2 } from "lucide-react";
import { useId, useState } from "react";

import type { StorageHybridStatus } from "@/api/admin/storage/type.d";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { parseHybridLimit } from "@/utils/admin/storage-hybrid";

type StorageHybridControlProps = {
  /** 从全局接口获取的策略状态 */
  status: StorageHybridStatus | null;
  /** 当前账号可以变更存储配置 */
  canWrite: boolean;
  /** 状态变更请求进行中 */
  saving: boolean;
  /** 提交策略与临时存储限额 */
  onChange: (enabled: boolean, limitBytes?: number) => Promise<void>;
};

/** 存储工具条的开关及开启、关闭确认对话框。 */
export function StorageHybridControl({
  status,
  canWrite,
  saving,
  onChange,
}: StorageHybridControlProps) {
  const [dialog, setDialog] = useState({ enabling: true, open: false });
  const [limit, setLimit] = useState("0");
  const label = useId();
  const limitId = useId();
  const helpId = useId();
  const active = status?.state === "active";
  const preparing = status?.state === "preparing";
  const { enabling } = dialog;
  const open = dialog.open && enabling !== active;
  const bytes = parseHybridLimit(limit);
  const closeDialog = () => setDialog({ enabling, open: false });

  if (dialog.open && !open) closeDialog();

  if (!status || (!status.has_cloud && !active && !preparing)) return null;

  return (
    <>
      <div className="flex shrink-0 items-center gap-2">
        <span id={label} className="text-sm font-medium">
          混合策略
        </span>
        {preparing && <Loader2 aria-label="正在检查并同步" className="size-3.5 animate-spin" />}
        <Switch
          aria-labelledby={label}
          checked={active}
          disabled={!canWrite || saving || preparing}
          onCheckedChange={() => {
            setLimit("0");
            setDialog({ enabling: !active, open: true });
          }}
        />
      </div>

      <Dialog
        open={open}
        onOpenChange={(next) => {
          if (!next && !saving) closeDialog();
        }}
      >
        <DialogContent showCloseButton={false} className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{enabling ? "开启混合策略？" : "关闭混合策略？"}</DialogTitle>
            <DialogDescription>
              {enabling
                ? "先检查存储配置并补齐本地缺失的云端素材。新素材保存在本地，模型需要外链时才暂存参考素材，以减少长期云端占用。"
                : "恢复原来的默认存储和访问方式，解冻相关存储操作。现有本地文件和云端文件会保留。"}
            </DialogDescription>
          </DialogHeader>

          {enabling && (
            <div className="grid gap-2">
              <label htmlFor={limitId} className="font-medium">
                云端临时素材限额（GB）
              </label>
              <Input
                id={limitId}
                inputMode="decimal"
                autoComplete="off"
                value={limit}
                onChange={(event) => setLimit(event.target.value)}
                disabled={saving || preparing}
                aria-invalid={bytes === null}
                aria-describedby={helpId}
              />
              <p id={helpId} className="text-muted-foreground text-sm">
                {bytes === null ? "请输入有效的非负数。" : "0 表示不限额。"}
              </p>
              {preparing && (
                <p role="status" className="text-muted-foreground flex items-center gap-2">
                  <Loader2 className="size-4 animate-spin" />
                  正在检查并同步云端素材，可以关闭弹窗等待完成。
                </p>
              )}
              {status.state === "failed" && status.error && (
                <p role="alert" className="text-destructive text-sm break-words">
                  {status.error}
                </p>
              )}
            </div>
          )}

          <DialogFooter>
            <Button variant="outline" disabled={saving} onClick={closeDialog}>
              {preparing ? "关闭弹窗" : "取消"}
            </Button>
            {!preparing && (
              <Button
                disabled={saving || (enabling && bytes === null)}
                onClick={() => {
                  void onChange(enabling, bytes ?? 0);
                }}
              >
                {saving && <Loader2 className="animate-spin" />}
                确定
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
