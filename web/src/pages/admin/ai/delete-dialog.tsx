import { useCallback, useEffect, useState } from "react";
import { CircleAlert, Loader2, Lock, Trash2 } from "lucide-react";
import { Link } from "react-router";
import { toast } from "sonner";

import { checkDelete, deleteTarget, setModelEnabled } from "@/api/admin-ai";
import type {
  ChannelView,
  DeleteBlocker,
  DeleteCheckResult,
  DeleteTarget,
  PluginView,
} from "@/api/admin-ai/type.d";
import { NativeSelect } from "@/components/admin-ui/native-select";
import { Notice } from "@/components/admin-ui/notice";
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
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { openDialog, type DialogControl } from "@/store/dialog";
import { errorMessage } from "@/utils/admin/errors";
import { channelHealth } from "@/utils/admin/health";

import { migrateModelsToChannel } from "./models/migrate-channel";
import { useAliveRef } from "../use-admin";

const TARGET_LABEL: Record<DeleteTarget, string> = {
  model: "模型",
  channel: "渠道",
  plugin: "插件",
};

/** 删除后一起删掉的东西 / 不受影响的东西，写在确认区里 */
const CASCADE: Record<DeleteTarget, { gone: string; kept: string }> = {
  model: {
    gone: "草稿与全部历史版本（彻底删除，不能恢复；之后可以用同一个标识重新导入）",
    kept: "历史生成任务照常可以查看",
  },
  channel: { gone: "渠道的 Key（加密存储）", kept: "历史生成任务照常可以查看" },
  plugin: { gone: "插件的全部版本代码", kept: "历史生成任务照常可以查看" },
};

export type DeleteDialogProps = {
  target: DeleteTarget;
  /** 对象 key */
  objectKey: string;
  /** 显示名 */
  name: string;
  /** 渠道迁移的候选（删渠道时用），打开时的快照 */
  channels?: ChannelView[];
  plugins?: PluginView[];
  /** 删除成功后（刷新列表、改选中项） */
  onDeleted: () => void;
  /** 迁移等就地处理改了别的数据后（刷新模型列表） */
  onChanged?: () => void;
};

/**
 * 删除对话框（插件 / 渠道 / 模型共用，走全局弹窗 store）：
 * 1. 打开先调预检，列出谁在引用它；
 * 2. 有阻断就不让删，但就地给出路：模型“下架并删除”，渠道“把这些模型迁到别的渠道”，
 *    插件跳到在用的渠道；处理完自动重新预检；
 * 3. 没有阻断才显示确认区：模型直接确认；渠道、插件影响面大，要输入名称 / key 才能删。
 * 预检只给界面看，后端删除时在事务里会再判断一次，那时的 409 原因也留在框里。
 */
function DeleteDialog({
  target,
  objectKey,
  name,
  channels = [],
  plugins = [],
  onDeleted,
  onChanged,
  open,
  onClose,
  onExited,
}: DeleteDialogProps & DialogControl) {
  const aliveRef = useAliveRef();
  const [check, setCheck] = useState<DeleteCheckResult | null>(null);
  const [checkError, setCheckError] = useState<string | null>(null);
  const [typed, setTyped] = useState("");
  const [busy, setBusy] = useState<"delete" | "disable" | "migrate" | null>(null);
  const [error, setError] = useState<string | null>(null);

  const runCheck = useCallback(async () => {
    setCheckError(null);
    try {
      const next = await checkDelete(target, objectKey);
      if (aliveRef.current) setCheck(next);
    } catch (cause) {
      if (aliveRef.current) setCheckError(errorMessage(cause, "没能检查引用情况"));
    }
  }, [aliveRef, target, objectKey]);

  useEffect(() => {
    void runCheck();
  }, [runCheck]);

  // 渠道和插件要输入名称确认；渠道用显示名，插件用 key（显示名可能重复）
  const confirmText = target === "channel" ? name : target === "plugin" ? objectKey : null;
  const blocked = !!check && check.blockers.length > 0;
  const canDelete = !!check && !blocked && (!confirmText || typed.trim() === confirmText);

  const remove = async (disableFirst = false) => {
    setError(null);
    setBusy(disableFirst ? "disable" : "delete");
    try {
      if (disableFirst) await setModelEnabled(objectKey, false);
      await deleteTarget(target, objectKey);
      toast.success(`已删除${TARGET_LABEL[target]}「${name}」`);
      onDeleted();
      onClose();
    } catch (cause) {
      // 全局 toast 已弹；原因留在框里，并重新预检（可能刚被别人引用）
      if (!aliveRef.current) return;
      setError(errorMessage(cause, "删除失败"));
      void runCheck();
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  const migrate = async (keys: string[], to: string) => {
    setError(null);
    setBusy("migrate");
    try {
      const result = await migrateModelsToChannel(keys, to);
      const toName = channels.find((item) => item.key === to)?.name ?? to;
      if (result.done.length) toast.success(`已把 ${result.done.length} 个模型迁到「${toName}」`);
      if (result.failed.length && aliveRef.current) {
        setError(result.failed.map((item) => `${item.key}：${item.reason}`).join("；"));
      }
      onChanged?.();
      await runCheck();
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && !busy && onClose()}
      onOpenChangeComplete={(next) => !next && onExited()}
    >
      <DialogContent className="sm:max-w-lg">
        <DialogHeader className="flex-row items-start gap-3">
          <span
            className={cn(
              "grid size-9 shrink-0 place-items-center rounded-full",
              blocked ? "bg-amber-500/15 text-amber-600" : "bg-destructive/10 text-destructive",
            )}
          >
            {blocked ? <Lock className="size-4" /> : <Trash2 className="size-4" />}
          </span>
          <div className="flex min-w-0 flex-col gap-1.5">
            <DialogTitle>
              {blocked ? "暂时不能删除" : `删除${TARGET_LABEL[target]}？`}「{name}」
            </DialogTitle>
            <DialogDescription>
              {!check
                ? "正在检查有没有别的地方在用它…"
                : blocked
                  ? "先处理下面的问题，处理完这里会自动刷新。"
                  : "删除后不能撤销。"}
            </DialogDescription>
          </div>
        </DialogHeader>

        {!check && !checkError && <Skeleton className="h-20" />}
        {checkError && (
          <Notice
            tone="danger"
            action={
              <Button size="xs" variant="outline" onClick={() => void runCheck()}>
                重试
              </Button>
            }
          >
            {checkError}
          </Notice>
        )}

        {blocked && (
          <ul className="flex flex-col gap-2">
            {check.blockers.map((blocker) => (
              <BlockerItem
                key={blocker.kind}
                blocker={blocker}
                target={target}
                objectKey={objectKey}
                channels={channels}
                plugins={plugins}
                busy={busy}
                onDisableAndDelete={() => void remove(true)}
                onMigrate={(keys, to) => void migrate(keys, to)}
              />
            ))}
          </ul>
        )}

        {check && !blocked && (
          <>
            <div className="bg-muted/40 grid gap-1.5 rounded-lg border p-3 text-xs">
              <div>
                <span className="text-muted-foreground">一起删除：</span>
                {CASCADE[target].gone}
              </div>
              <div>
                <span className="text-muted-foreground">不受影响：</span>
                {CASCADE[target].kept}
              </div>
            </div>
            {confirmText && (
              <label className="flex flex-col gap-1.5 text-sm">
                <span>
                  输入 <code className="bg-muted rounded px-1 font-mono">{confirmText}</code> 确认
                </span>
                <Input
                  value={typed}
                  autoComplete="off"
                  aria-label="输入名称确认删除"
                  onChange={(event) => setTyped(event.target.value)}
                />
              </label>
            )}
          </>
        )}

        {error && <Notice tone="danger">{error}</Notice>}

        <DialogFooter>
          <Button variant="outline" disabled={!!busy} onClick={onClose}>
            {blocked ? "关闭" : "取消"}
          </Button>
          {!blocked && (
            <Button
              variant="destructive"
              disabled={!canDelete || !!busy}
              onClick={() => void remove()}
            >
              {busy === "delete" ? <Loader2 className="animate-spin" /> : <Trash2 />}
              删除
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** 一条阻断原因 + 就地处理 */
function BlockerItem({
  blocker,
  target,
  objectKey,
  channels,
  plugins,
  busy,
  onDisableAndDelete,
  onMigrate,
}: {
  blocker: DeleteBlocker;
  target: DeleteTarget;
  objectKey: string;
  channels: ChannelView[];
  plugins: PluginView[];
  busy: "delete" | "disable" | "migrate" | null;
  onDisableAndDelete: () => void;
  onMigrate: (keys: string[], to: string) => void;
}) {
  const others = channels.filter((item) => item.key !== objectKey);
  const [to, setTo] = useState(others[0]?.key ?? "");

  return (
    <li className="rounded-lg border border-amber-500/30 bg-amber-500/5 p-3 text-sm">
      <div className="flex items-start gap-2">
        <CircleAlert className="mt-0.5 size-4 shrink-0 text-amber-600" />
        <span className="flex-1">{blocker.message}</span>
      </div>

      {blocker.kind === "model_enabled" && target === "model" && (
        <div className="mt-2 flex justify-end">
          <Button size="sm" variant="outline" disabled={!!busy} onClick={onDisableAndDelete}>
            {busy === "disable" && <Loader2 className="animate-spin" />}
            下架并删除
          </Button>
        </div>
      )}

      {blocker.kind === "channel_models" && (
        <div className="mt-2.5 flex flex-col gap-2 border-t pt-2.5">
          <div className="flex flex-wrap gap-1">
            {blocker.refs.map((ref) => (
              <Link
                key={ref.key}
                to={`/admin/ai/models?key=${encodeURIComponent(ref.key)}`}
                className="bg-background hover:bg-accent rounded-md border px-2 py-0.5 text-xs"
              >
                {ref.name || ref.key}
              </Link>
            ))}
          </div>
          {others.length > 0 ? (
            <>
              <div className="flex items-center gap-2">
                <span className="text-muted-foreground shrink-0 text-xs">全部迁到</span>
                <NativeSelect
                  className="h-8 text-xs"
                  aria-label="迁移到的渠道"
                  value={to}
                  onChange={(event) => setTo(event.target.value)}
                >
                  {others.map((item) => (
                    <option key={item.key} value={item.key}>
                      {item.name}（{channelHealth(item, plugins).label}）
                    </option>
                  ))}
                </NativeSelect>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={!to || !!busy}
                  onClick={() =>
                    onMigrate(
                      blocker.refs.map((ref) => ref.key),
                      to,
                    )
                  }
                >
                  {busy === "migrate" && <Loader2 className="animate-spin" />}
                  迁移
                </Button>
              </div>
              <p className="text-muted-foreground text-xs">
                已上线的模型会用新渠道重新发布；有未上线修改的模型，这些修改会一起上线。
              </p>
            </>
          ) : (
            <p className="text-muted-foreground text-xs">没有别的渠道可迁，先新建一个渠道。</p>
          )}
        </div>
      )}

      {blocker.kind === "plugin_channels" && blocker.refs.length > 0 && (
        <div className="mt-2.5 flex flex-wrap gap-1 border-t pt-2.5">
          {blocker.refs.map((ref) => (
            <Link
              key={ref.key}
              to={`/admin/ai/channels?key=${encodeURIComponent(ref.key)}`}
              className="bg-background hover:bg-accent rounded-md border px-2 py-0.5 text-xs"
            >
              {ref.name || ref.key}
            </Link>
          ))}
        </div>
      )}
    </li>
  );
}

/** 打开删除对话框 */
export const openDeleteDialog = (props: DeleteDialogProps) => openDialog(DeleteDialog, props);
