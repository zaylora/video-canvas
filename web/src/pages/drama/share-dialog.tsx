import type { CanvasVersion } from "@/utils/canvas/collaboration-version";

import { useEffect, useState } from "react";
import {
  ArrowRightLeft,
  Copy,
  Ellipsis,
  Eye,
  FolderPlus,
  Pencil,
  UserRoundMinus,
  Zap,
} from "lucide-react";
import { toast } from "sonner";
import {
  setCanvasGenerationPermission,
  setCanvasLibraryPermission,
  setCollaborationMember,
} from "@/api/canvas/collaboration";
import { canvasSharing, dramaSharing, type SharingMode, type Viewer } from "@/api/drama/share";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
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
  editor: "对方可以与其他成员实时编辑同一张画布，查看节点、提示词和媒体。",
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

const collaborationModes = [
  modes[0],
  { mode: "editor", label: "协作模式（可编辑）", Icon: Pencil, detail: "", action: "邀请编辑" },
  modes[1],
  modes[2],
] as const;

/**
 * 短剧共享弹窗：仅可查看 / 发送副本 / 转移所有权，右侧列出可查看的人。
 * 用 openDialog 打开（store/dialog），关闭时播完退出动画再移除。
 */
export function DramaShareDialog({
  id,
  title,
  revision,
  collaborationRevision,
  transferred,
  open,
  onClose,
  onExited,
}: {
  id: string;
  title: string;
  revision?: CanvasVersion;
  collaborationRevision?: () => Promise<CanvasVersion>;
  transferred: () => void;
} & DialogControl) {
  const canvas = revision !== undefined || collaborationRevision !== undefined;
  const collaborative = collaborationRevision !== undefined;
  const api = canvas ? canvasSharing : dramaSharing;
  const options = collaborationRevision ? collaborationModes : modes;
  const [mode, setMode] = useState<SharingMode | "editor">("view");
  const [email, setEmail] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [confirmedRevision, setConfirmedRevision] = useState<CanvasVersion>();
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
    setBusy(true);
    try {
      const address = email.trim();
      if (mode === "transfer" && !confirm) {
        setConfirmedRevision(collaborationRevision ? await collaborationRevision() : revision);
        setConfirm(true);
        return;
      }
      const invite = mode === "view" || mode === "editor";
      if (mode === "editor" || (collaborationRevision && mode === "view")) {
        await setCollaborationMember(id, address, mode === "editor" ? "editor" : "viewer");
      } else {
        const version =
          mode === "transfer"
            ? confirmedRevision
            : mode === "copy" && collaborationRevision
              ? await collaborationRevision()
              : revision;
        await api.submit(id, mode, address, version);
      }
      toast.success(
        mode === "editor"
          ? "已授予编辑权限"
          : mode === "view"
            ? "已授予查看权限"
            : mode === "copy"
              ? "已发送副本"
              : "已转移所有权",
        canvas && !invite
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
  const changeMemberRole = async (item: Viewer) => {
    const role = item.role === "editor" ? "viewer" : "editor";
    setBusy(true);
    try {
      await setCollaborationMember(id, item.email, role);
      setMembers(
        (items) =>
          items?.map((member) =>
            member.id === item.id
              ? { ...member, role, canGenerate: false, canCopyToLibrary: false }
              : member,
          ) ?? null,
      );
      toast.success(role === "editor" ? "已授予编辑权限" : "已改为仅可查看");
    } finally {
      setBusy(false);
    }
  };
  const changeGeneration = async (item: Viewer) => {
    const canGenerate = !item.canGenerate;
    setBusy(true);
    try {
      await setCanvasGenerationPermission(id, item.id, canGenerate);
      setMembers(
        (items) =>
          items?.map((member) => (member.id === item.id ? { ...member, canGenerate } : member)) ??
          null,
      );
      toast.success(canGenerate ? "已授予生成权限，使用该成员自己的积分" : "已收回生成权限");
    } finally {
      setBusy(false);
    }
  };
  const changeLibraryPermission = async (item: Viewer) => {
    const canCopyToLibrary = !item.canCopyToLibrary;
    setBusy(true);
    try {
      await setCanvasLibraryPermission(id, item.id, canCopyToLibrary);
      setMembers(
        (items) =>
          items?.map((member) =>
            member.id === item.id ? { ...member, canCopyToLibrary } : member,
          ) ?? null,
      );
      toast.success(
        canCopyToLibrary
          ? "已允许将画布素材保存为个人副本"
          : "已收回收藏权限，已保存的副本仍可使用",
      );
    } finally {
      setBusy(false);
    }
  };
  const remove = async (member: string) => {
    setBusy(true);
    try {
      await api.remove(id, member);
      setMembers((items) => items?.filter((item) => item.id !== member) ?? null);
      toast.success(collaborationRevision ? "已收回访问权限" : "已收回查看权限");
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
        className={cn(
          "flex max-h-[min(90dvh,680px)] flex-col gap-0 overflow-hidden p-0 [corner-shape:squircle] sm:max-w-4xl",
          !collaborative && "h-[min(90dvh,620px)]",
        )}
      >
        <DialogHeader className="shrink-0 gap-1.5 px-6 pt-6 pb-5 sm:px-8">
          <DialogTitle className="truncate pr-8 text-xl font-semibold tracking-tight">
            {confirm ? "确认转移所有权" : `共享《${title}》`}
          </DialogTitle>
          <DialogDescription>
            {confirm
              ? `将《${title}》转移给 ${email.trim()}。你的账号将移除${canvas ? "原画布" : "该剧"}，${members ? `现有 ${members.length} 位${collaborationRevision ? "成员" : "查看者"}` : collaborationRevision ? "已有成员" : "已有查看者"}的权限将被收回。`
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
          <div className="border-chrome-border grid min-h-0 flex-1 grid-cols-[minmax(0,1fr)] content-start overflow-x-hidden overflow-y-auto overscroll-contain border-y [scrollbar-width:thin] sm:grid-cols-[minmax(0,1fr)_minmax(0,34%)]">
            <div className="min-w-0 px-5 py-5 sm:px-7">
              {confirm ? (
                <div className="ring-chrome-border flex items-center gap-4 rounded-2xl p-5 ring-1 [corner-shape:squircle]">
                  <ArrowRightLeft className="text-destructive size-5 shrink-0" aria-hidden="true" />
                  <div className="min-w-0">
                    <p className="text-muted-foreground text-xs">接收方</p>
                    <p className="truncate text-base font-medium">{email.trim()}</p>
                    {collaborationRevision && (
                      <p className="text-muted-foreground text-xs">
                        已同步版本 {confirmedRevision}
                      </p>
                    )}
                  </div>
                </div>
              ) : (
                <fieldset disabled={busy} className="grid min-w-0 gap-5">
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
                  <fieldset className={cn("grid min-w-0 gap-2", collaborative && "md:grid-cols-2")}>
                    <legend className="text-muted-foreground mb-2 text-xs">共享方式</legend>
                    {options.map(({ mode: value, label, detail, Icon }) => (
                      <label
                        key={value}
                        className={cn(
                          "ring-chrome-border has-focus-visible:ring-ring/50 flex min-w-0 cursor-pointer items-start gap-2.5 rounded-xl px-3 py-3 ring-1 transition-colors [corner-shape:squircle] has-focus-visible:ring-2",
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
            <section className="border-chrome-border flex min-w-0 flex-col border-t px-5 py-5 sm:border-t-0 sm:border-l">
              <h3 className="mb-2 flex items-center gap-2 text-sm font-medium">
                {collaborationRevision
                  ? "协作成员"
                  : canvas
                    ? "可查看原画布的人"
                    : "可查看此剧的人"}
                {members && (
                  <span className="bg-chrome-hover text-muted-foreground rounded-full px-2 py-0.5 text-xs tabular-nums">
                    {members.length}
                  </span>
                )}
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
                <p className="text-muted-foreground text-sm">
                  {collaborationRevision ? "暂无成员" : "暂无查看成员"}
                </p>
              ) : (
                members.map((item) => {
                  const name = canvas ? item.username?.trim() || "账号不可用" : item.email;
                  return (
                    <div
                      key={item.id}
                      className="border-chrome-border flex min-h-12 items-center gap-2.5 border-b py-2.5 last:border-b-0"
                    >
                      <span
                        aria-hidden="true"
                        className="bg-chrome-hover ring-chrome-border grid size-8 shrink-0 place-items-center rounded-full text-xs font-medium uppercase ring-1"
                      >
                        {Array.from(name)[0]}
                      </span>
                      <div className="min-w-0 flex-1 text-sm">
                        <div className="font-medium wrap-anywhere">{name}</div>
                        {collaborative && (
                          <div className="text-muted-foreground mt-0.5 flex flex-wrap items-center gap-x-2 text-xs">
                            {item.role === "editor" ? "可编辑" : "仅可查看"}
                            {item.role === "editor" && item.canGenerate && <span>可生成</span>}
                            {item.role === "editor" && item.canCopyToLibrary && (
                              <span>可收藏素材</span>
                            )}
                          </div>
                        )}
                      </div>
                      {collaborative ? (
                        <DropdownMenu modal={false}>
                          <DropdownMenuTrigger
                            aria-label={`管理${name}的权限`}
                            disabled={busy || confirm}
                            render={
                              <Button
                                type="button"
                                variant="ghost"
                                size="icon-sm"
                                className="text-muted-foreground rounded-full"
                              />
                            }
                          >
                            <Ellipsis />
                          </DropdownMenuTrigger>
                          <DropdownMenuContent
                            align="end"
                            className="bg-chrome ring-chrome-border w-40 backdrop-blur-xl backdrop-saturate-150"
                          >
                            <DropdownMenuItem
                              onClick={() => void changeMemberRole(item).catch(() => undefined)}
                            >
                              {item.role === "editor" ? <Eye /> : <Pencil />}
                              {item.role === "editor" ? "改为仅查看" : "允许编辑"}
                            </DropdownMenuItem>
                            {item.role === "editor" && (
                              <>
                                <DropdownMenuItem
                                  onClick={() => void changeGeneration(item).catch(() => undefined)}
                                >
                                  <Zap />
                                  {item.canGenerate ? "收回生成权限" : "允许生成"}
                                </DropdownMenuItem>
                                <DropdownMenuItem
                                  onClick={() =>
                                    void changeLibraryPermission(item).catch(() => undefined)
                                  }
                                >
                                  <FolderPlus />
                                  {item.canCopyToLibrary ? "收回收藏权限" : "允许收藏素材"}
                                </DropdownMenuItem>
                              </>
                            )}
                            <DropdownMenuSeparator />
                            <DropdownMenuItem
                              variant="destructive"
                              onClick={() => void remove(item.id).catch(() => undefined)}
                            >
                              <UserRoundMinus />
                              移除成员
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      ) : (
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          disabled={busy || confirm}
                          className="text-muted-foreground rounded-full"
                          onClick={() => void remove(item.id).catch(() => undefined)}
                        >
                          移除
                        </Button>
                      )}
                    </div>
                  );
                })
              )}
            </section>
          </div>
          <div className="flex shrink-0 flex-wrap justify-end gap-2 px-5 py-4 sm:px-7">
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
                  : options.find((item) => item.mode === mode)?.action}
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  );
}
