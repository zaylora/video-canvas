import { useEffect, useState } from "react";
import { ArrowRightLeft, Copy, Eye } from "lucide-react";
import { toast } from "sonner";
import { canvasSharing, dramaSharing, type SharingMode, type Viewer } from "@/api/drama/share";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/admin-ui/dialog";
import { cn } from "@/lib/utils";
import type { DialogControl } from "@/store/dialog";

const canvasDetails = {
  view: "对方能查看已保存的节点、提示词和媒体。你可以随时收回权限。",
  copy: "发送可独立编辑的画布和素材。此后双方各自修改。",
  transfer: "对方接手画布。你和现有查看者将无法再打开原画布。",
};

const modes = [
  {
    mode: "view",
    label: "仅可查看",
    Icon: Eye,
    detail: "对方从「共享给我」进入完整工作台，查看最新资产和分集。你可以随时收回权限。",
    action: "授予查看权限",
  },
  {
    mode: "copy",
    label: "发送副本",
    Icon: Copy,
    detail: "对方在「我的短剧」收到可继续创作的完整副本，双方各自编辑。",
    action: "发送副本",
  },
  {
    mode: "transfer",
    label: "转移所有权",
    Icon: ArrowRightLeft,
    detail: "对方接手创作，你的账号将移除这部短剧。",
    action: "核对转移信息",
  },
] as const;

/**
 * 短剧共享弹窗：仅可查看 / 发送副本 / 转移所有权，右侧列出可查看的人。
 * 用 openDialog 打开（store/dialog），关闭时播完退出动画再移除。
 */
export function DramaShareDialog({
  id,
  title,
  revision,
  transferred,
  open,
  onClose,
  onExited,
}: {
  id: string;
  title: string;
  revision?: number;
  transferred: () => void;
} & DialogControl) {
  const canvas = revision !== undefined;
  const api = canvas ? canvasSharing : dramaSharing;
  const [mode, setMode] = useState<SharingMode>("view");
  const [email, setEmail] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [members, setMembers] = useState<Viewer[] | null>(null);
  const [failed, setFailed] = useState(false);
  const [reload, setReload] = useState(0);
  useEffect(() => {
    let active = true;
    void api.members(id).then(
      (items) => {
        if (active) {
          setMembers(items);
          setFailed(false);
        }
      },
      () => {
        if (active) setFailed(true);
      },
    );
    return () => {
      active = false;
    };
  }, [api, id, reload]);
  const submit = async () => {
    if (mode === "transfer" && !confirm) {
      setConfirm(true);
      return;
    }
    setBusy(true);
    try {
      await api.submit(id, mode, email.trim(), revision);
      toast.success(
        mode === "view" ? "已授予查看权限" : mode === "copy" ? "已发送副本" : "已转移所有权",
        api === canvasSharing && mode !== "view"
          ? { description: "对方可在「我的作品 → 个人画布」继续编辑。" }
          : undefined,
      );
      if (mode === "transfer") transferred();
      onClose();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "共享失败，请重试");
      setConfirm(false);
    } finally {
      setBusy(false);
    }
  };
  const remove = async (member: string) => {
    setBusy(true);
    try {
      await api.remove(id, member);
      setMembers((items) => items?.filter((item) => item.id !== member) ?? null);
      toast.success("已收回查看权限");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !busy) onClose();
      }}
      onOpenChangeComplete={(next) => !next && onExited()}
    >
      <DialogContent
        surface="glass"
        className="flex h-[min(90dvh,620px)] flex-col gap-0 overflow-hidden p-0 [corner-shape:squircle] sm:max-w-4xl"
      >
        <DialogHeader className="gap-1.5 px-6 pt-8 pb-6 sm:px-8">
          <DialogTitle className="truncate pr-8 text-xl font-semibold tracking-tight">
            {confirm ? "确认转移所有权" : `共享《${title}》`}
          </DialogTitle>
          <DialogDescription>
            {confirm
              ? `将《${title}》转移给 ${email.trim()}。你的账号将移除${canvas ? "原画布" : "该剧"}，${members ? `现有 ${members.length} 位查看者` : "已有查看者"}的权限将被收回。`
              : canvas
                ? "用接收方的注册邮箱共享这张画布。"
                : "用接收方的注册邮箱共享这部短剧。"}
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex min-h-0 flex-1 flex-col"
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <div className="border-chrome-border grid min-h-0 flex-1 overflow-y-auto border-y sm:grid-cols-[minmax(0,1fr)_300px] sm:overflow-hidden">
            <div className="min-h-0 px-6 py-6 sm:overflow-y-auto sm:px-8">
              {confirm ? (
                <div className="ring-chrome-border flex items-center gap-4 rounded-2xl p-5 ring-1 [corner-shape:squircle]">
                  <ArrowRightLeft className="text-destructive size-5 shrink-0" aria-hidden="true" />
                  <div className="min-w-0">
                    <p className="text-muted-foreground text-xs">接收方</p>
                    <p className="truncate text-base font-medium">{email.trim()}</p>
                  </div>
                </div>
              ) : (
                <fieldset disabled={busy} className="grid gap-6">
                  <label className="grid gap-2">
                    <span className="text-muted-foreground text-xs">接收方邮箱</span>
                    <Input
                      type="email"
                      required
                      maxLength={128}
                      placeholder="name@example.com"
                      value={email}
                      onChange={(event) => setEmail(event.target.value)}
                      className="bg-chrome-hover border-chrome-border h-11 rounded-full px-4"
                    />
                  </label>
                  <fieldset className="grid gap-2">
                    <legend className="text-muted-foreground mb-2 text-xs">共享方式</legend>
                    {modes.map(({ mode: value, label, detail, Icon }) => (
                      <label
                        key={value}
                        className={cn(
                          "ring-chrome-border has-focus-visible:ring-ring/50 flex cursor-pointer items-start gap-3.5 rounded-2xl px-4 py-3.5 ring-1 transition-colors [corner-shape:squircle] has-focus-visible:ring-2",
                          mode === value
                            ? "bg-chrome-hover ring-foreground/40"
                            : "hover:bg-chrome-hover",
                        )}
                      >
                        <input
                          type="radio"
                          name="mode"
                          className="sr-only"
                          checked={mode === value}
                          onChange={() => setMode(value)}
                        />
                        <Icon
                          className={cn(
                            "mt-0.5 size-5 shrink-0",
                            mode === value ? "text-foreground" : "text-muted-foreground",
                          )}
                          aria-hidden="true"
                        />
                        <span className="grid gap-1">
                          <span className="text-sm font-medium">{label}</span>
                          <span className="text-muted-foreground text-xs leading-5">
                            {canvas ? canvasDetails[value] : detail}
                          </span>
                        </span>
                      </label>
                    ))}
                  </fieldset>
                </fieldset>
              )}
            </div>
            <section className="border-chrome-border flex min-h-0 flex-col border-t px-6 py-6 sm:overflow-y-auto sm:border-t-0 sm:border-l">
              <h3 className="text-muted-foreground mb-2 text-xs">
                {canvas ? "可查看原画布的人" : "可查看此剧的人"}
                {members && members.length > 0 ? ` · ${members.length}` : ""}
              </h3>
              {failed ? (
                <Button
                  type="button"
                  variant="ghost"
                  className="self-start rounded-full"
                  onClick={() => {
                    setFailed(false);
                    setReload((value) => value + 1);
                  }}
                >
                  重新加载成员
                </Button>
              ) : !members ? (
                <p className="text-muted-foreground text-sm">加载中…</p>
              ) : members.length === 0 ? (
                <p className="text-muted-foreground text-sm">暂无查看成员</p>
              ) : (
                members.map((item) => (
                  <div key={item.id} className="flex min-h-11 items-center gap-3">
                    <span className="bg-chrome-hover ring-chrome-border grid size-8 shrink-0 place-items-center rounded-full text-xs font-medium uppercase ring-1">
                      {item.email.slice(0, 1)}
                    </span>
                    <span className="min-w-0 flex-1 truncate text-sm" title={item.email}>
                      {item.email}
                    </span>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      className="text-muted-foreground rounded-full"
                      disabled={busy || confirm}
                      onClick={() => void remove(item.id).catch(() => undefined)}
                    >
                      移除
                    </Button>
                  </div>
                ))
              )}
            </section>
          </div>
          <div className="flex justify-end gap-2 px-6 py-5 sm:px-8">
            <Button
              type="button"
              variant="ghost"
              className="h-10 rounded-full px-5"
              disabled={busy}
              onClick={() => (confirm ? setConfirm(false) : onClose())}
            >
              {confirm ? "返回核对" : "取消"}
            </Button>
            <Button
              type="submit"
              variant={confirm ? "destructive" : "default"}
              className="h-10 rounded-full px-5"
              disabled={busy}
            >
              {busy
                ? "处理中…"
                : confirm
                  ? "确认转移"
                  : modes.find((item) => item.mode === mode)?.action}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
