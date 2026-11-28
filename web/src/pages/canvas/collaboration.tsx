import type { CanvasVersion } from "@/utils/canvas/collaboration-version";

import { ReactFlowProvider } from "@xyflow/react";
import {
  ArrowLeft,
  CircleAlert,
  FileDown,
  FolderOpen,
  Hand,
  History,
  MessageSquare,
  MousePointer2,
  Redo2,
  Sparkles,
  Undo2,
  Users,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, Navigate, useNavigate, useParams } from "react-router";

import { getCollaboration } from "@/api/canvas/collaboration";
import { CanvasModeDialog } from "@/pages/canvas/chrome/canvas-mode-dialog";
import {
  ChromeButton,
  ChromePill,
  ChromeSegment,
  ChromeTooltip,
} from "@/components/canvas/chrome/chrome";
import { TransferProgressDialog } from "@/components/canvas/transfer-progress-dialog";
import { CanvasEditor } from "@/components/canvas-editor/canvas-editor";
import {
  CanvasGenerationProgressLayer,
  CanvasPresenceLayer,
  CanvasPresenceRoster,
} from "@/components/canvas-editor/collaboration-presence";
import { useCanvasEditor } from "@/components/canvas-editor/editor-api";
import { DialogSession } from "@/components/dialog-session";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/admin-ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { useCanvasCollaboration } from "@/hooks/use-canvas-collaboration";
import { useCanvasExport } from "@/hooks/use-canvas-export";
import { usePageTitle } from "@/hooks/use-page-title";
import { cn } from "@/lib/utils";
import { canvasSharing } from "@/api/drama/share";
import { DramaShareDialog } from "@/pages/drama/share-dialog";
import { openDialog } from "@/store/dialog";
import { ApiError } from "@/utils/requests/request";
import { canvasPageTitle } from "@/utils/page-title";
import { CanvasGenerationContext, CanvasLibraryContext } from "@/utils/canvas/canvas-id";
import { pruneCanvasCollaborationDrafts } from "@/utils/canvas/collaboration-session";
import { getCurrentUserId } from "@/utils/storage/user-id";
import { CanvasRoomPresence } from "@/utils/ws/canvas-presence";

import { buildAddNodeItems } from "./chrome/add-node-items";
import { AddNodeButton } from "./chrome/add-node-button";
import { ShortcutsDialog } from "./chrome/shortcuts-dialog";
import { CreditsMenu } from "./chrome/top-right-bar";
import { WorkspaceChrome } from "./chrome/workspace-chrome";
import { CanvasCommentThread } from "./collaboration-comments";
import { CanvasMentionNotice } from "./collaboration-mentions";
import { CanvasVersionHistory } from "./collaboration-history";
import { AgentDock, AgentHostProvider, useAgentHost } from "./agent-host";

const sharedPath = "/canvases?scope=shared-canvas";
const personalPath = "/canvases?scope=mine";
const noLegacyConflict = () => undefined;
type Snapshot = Awaited<ReturnType<typeof getCollaboration>>;

export default function CollaborationPage() {
  const { id } = useParams();
  return id ? <OpenCollaboration key={id} id={id} /> : null;
}

function OpenCollaboration({ id }: { id: string }) {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [error, setError] = useState<"access" | "network" | null>(null);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    let active = true;
    void getCollaboration(id).then(
      (data) => {
        if (active) setSnapshot(data);
      },
      (failure: unknown) => {
        if (!active) return;
        const unavailable =
          failure instanceof ApiError && (failure.status === 404 || failure.code === 30018);
        if (unavailable) pruneCanvasCollaborationDrafts(getCurrentUserId(), id);
        setError(unavailable ? "access" : "network");
      },
    );
    return () => {
      active = false;
    };
  }, [id, retry]);
  if (snapshot) return <CollaborativeWorkspace key={snapshot.epoch} snapshot={snapshot} />;
  return (
    <main className="grid h-svh place-content-center justify-items-center gap-3 px-4 text-center">
      <p role="status">
        {error === "access"
          ? "协作画布不可访问或已关闭。"
          : error === "network"
            ? "暂时无法打开画布。"
            : "正在连接协作画布…"}
      </p>
      {error === "network" && (
        <Button
          onClick={() => {
            setError(null);
            setRetry((value) => value + 1);
          }}
        >
          重新连接
        </Button>
      )}
      {error && (
        <Link className="text-sm underline underline-offset-4" to={sharedPath}>
          返回画布列表
        </Link>
      )}
    </main>
  );
}

function CollaborativeWorkspace({ snapshot }: { snapshot: Snapshot }) {
  const [shortcutsOpen, setShortcutsOpen] = useState(false);
  const openShortcuts = useCallback(() => setShortcutsOpen(true), []);
  const {
    role,
    sessionEnded,
    canGenerate,
    canCopyToLibrary,
    editor,
    conflicts,
    resolve,
    undoResult,
    dismissUndo,
    discardAccess,
  } = useCanvasCollaboration(snapshot);
  const presence = useMemo(
    () => new CanvasRoomPresence(snapshot.canvas.id, snapshot.epoch),
    [snapshot.canvas.id, snapshot.epoch],
  );
  const { confirmGeneration, flush } = editor;
  const generation = useMemo(
    () => ({ canGenerate, epoch: snapshot.epoch, confirmGeneration }),
    [canGenerate, snapshot.epoch, confirmGeneration],
  );
  const library = useMemo(
    () =>
      role === "editor"
        ? { canvasId: snapshot.canvas.id, epoch: snapshot.epoch, allowed: canCopyToLibrary, flush }
        : null,
    [role, snapshot.canvas.id, snapshot.epoch, canCopyToLibrary, flush],
  );
  usePageTitle(canvasPageTitle(role ? editor.title : "画布不可访问"));
  if (role === "viewer") return <Navigate replace to={`/canvas/${snapshot.canvas.id}/reader`} />;
  if (!role) {
    const owner = snapshot.role === "owner";
    const canvasPath = `/canvas/${encodeURIComponent(snapshot.canvas.id)}`;
    return (
      <main className="grid h-svh place-content-center justify-items-center gap-3 px-4 text-center">
        <p role="status">
          {sessionEnded ? "当前协作会话已结束。" : "你已失去这张画布的协作权限。"}
        </p>
        {sessionEnded && (
          <Link
            className="text-sm underline underline-offset-4"
            to={owner ? canvasPath : `${canvasPath}/reader`}
          >
            {owner ? "重新打开画布" : "继续查看画布"}
          </Link>
        )}
        <Link
          className="text-sm underline underline-offset-4"
          to={owner ? personalPath : sharedPath}
        >
          返回画布列表
        </Link>
      </main>
    );
  }
  return (
    <ReactFlowProvider>
      <CanvasGenerationContext.Provider value={generation}>
        <CanvasLibraryContext.Provider value={library}>
          <AgentHostProvider
            canvasId={snapshot.canvas.id}
            collaboration={{ epoch: snapshot.epoch, flush: editor.flush }}
            enabled={canGenerate}
          >
            <CanvasEditor
              canvas={snapshot.canvas}
              collaboration={editor}
              collaborationPresence={presence}
              recovery={null}
              menuItems={buildAddNodeItems}
              aside={canGenerate ? <AgentDock /> : undefined}
              onConflict={noLegacyConflict}
              onOpenShortcuts={openShortcuts}
              overlay={
                <>
                  <CanvasGenerationProgressLayer
                    canvasId={snapshot.canvas.id}
                    epoch={snapshot.epoch}
                  />
                  <CollaborationChrome
                    canGenerate={canGenerate}
                    role={role}
                    conflicts={conflicts}
                    resolve={resolve}
                    undo={editor.undo}
                    redo={editor.redo}
                    undoResult={undoResult}
                    dismissUndo={dismissUndo}
                    presence={presence}
                    epoch={snapshot.epoch}
                    discardAccess={discardAccess}
                    openShortcuts={openShortcuts}
                  />
                </>
              }
            />
            <ShortcutsDialog
              open={shortcutsOpen}
              onOpenChange={setShortcutsOpen}
              scope="editor"
              canGenerate={canGenerate}
            />
          </AgentHostProvider>
        </CanvasLibraryContext.Provider>
      </CanvasGenerationContext.Provider>
    </ReactFlowProvider>
  );
}

function CollaborationChrome({
  canGenerate,
  role,
  conflicts,
  resolve,
  undo,
  redo,
  undoResult,
  dismissUndo,
  presence,
  epoch,
  discardAccess,
  openShortcuts,
}: Pick<
  ReturnType<typeof useCanvasCollaboration>,
  "conflicts" | "resolve" | "undoResult" | "dismissUndo"
> & {
  canGenerate: boolean;
  role: "owner" | "editor";
  undo: () => boolean;
  redo: () => boolean;
  presence: CanvasRoomPresence;
  epoch: CanvasVersion;
  discardAccess: () => void;
  openShortcuts: () => void;
}) {
  const { toggle } = useAgentHost();
  const editor = useCanvasEditor();
  const navigate = useNavigate();
  const [conflictsOpen, setConflictsOpen] = useState(false);
  const [showPresence, setShowPresence] = useState(true);
  const [commentNode, setCommentNode] = useState<string | null>(null);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [modeOpen, setModeOpen] = useState(false);
  const selectedNode = editor.nodes.find((node) => node.selected && node.type !== "group");
  const status = {
    loading: "连接中",
    saved: "已同步",
    saving: "保存中",
    error: "连接中断",
    conflict: "有冲突待处理",
  }[editor.saveStatus];
  const back = role === "owner" ? personalPath : sharedPath;
  return (
    <>
      <CanvasPresenceLayer presence={presence} visible={showPresence} />
      <WorkspaceChrome
        viewPreferences="editor"
        viewToolControls={
          <ChromeSegment
            id="collaboration-tool"
            value={editor.tool}
            onValueChange={editor.setTool}
            items={[
              { value: "select", label: "选择", shortcut: "V", icon: <MousePointer2 /> },
              { value: "pan", label: "抓手", shortcut: "H", icon: <Hand /> },
            ]}
          />
        }
        minimap={editor.minimap}
        onMinimapChange={editor.setMinimap}
        onOpenShortcuts={openShortcuts}
        onLocalViewChange={presence.stopFollowing}
        topRight={
          canGenerate && (
            <ChromePill>
              <CreditsMenu personal />
            </ChromePill>
          )
        }
        topLeft={
          <ChromePill
            className={cn(
              "h-auto min-h-10 flex-wrap rounded-2xl py-1 sm:rounded-full",
              canGenerate
                ? "max-w-[calc(100dvw-8rem)] sm:max-w-[calc(100dvw-12rem)]"
                : "max-w-[calc(100dvw-1.5rem)]",
            )}
          >
            <Link
              to={back}
              aria-label="返回画布列表"
              className="hover:bg-chrome-hover grid size-8 shrink-0 place-items-center rounded-full"
            >
              <ArrowLeft className="size-4" />
            </Link>
            <span className="max-w-40 truncate px-2 text-sm font-semibold">{editor.title}</span>
            <span role="status" className="text-muted-foreground px-2 text-xs">
              {status}
            </span>
            <CanvasPresenceRoster presence={presence} />
            <CanvasMentionNotice
              canvasId={editor.canvasId}
              epoch={epoch}
              open={(id) => {
                if (editor.locateNode(id)) setCommentNode(id);
              }}
            />
            <ChromeButton
              aria-label="查看所选节点的评论"
              disabled={!selectedNode}
              onClick={() => setCommentNode(selectedNode?.id ?? null)}
            >
              <MessageSquare />
            </ChromeButton>
            <ChromeButton aria-label="查看版本活动" onClick={() => setHistoryOpen(true)}>
              <History />
            </ChromeButton>
            <ChromeButton
              aria-label="显示协作者光标和选区"
              aria-pressed={showPresence}
              onClick={() => setShowPresence((visible) => !visible)}
            >
              <MousePointer2 />
            </ChromeButton>
            <ChromeButton
              aria-label="撤销我的上一次编辑"
              disabled={conflicts.length > 0}
              onClick={undo}
            >
              <Undo2 />
            </ChromeButton>
            <ChromeButton
              aria-label="重做我的上一次撤销"
              disabled={conflicts.length > 0}
              onClick={redo}
            >
              <Redo2 />
            </ChromeButton>
            <div className="flex shrink-0 items-center gap-0.5">
              <AddNodeButton
                side="bottom"
                size="default"
                onAdd={editor.addAtCenter}
                onUpload={editor.uploadAtCenter}
                onOpenAssets={editor.openAssetPicker}
              />
              <ChromeTooltip label="资产库" side="bottom">
                <ChromeButton aria-label="资产库" onClick={editor.openAssetPicker}>
                  <FolderOpen />
                </ChromeButton>
              </ChromeTooltip>
            </div>
            {role === "owner" && (
              <ChromeButton
                aria-label="共享画布和管理成员"
                onClick={() => {
                  openDialog(DramaShareDialog, {
                    id: editor.canvasId,
                    title: editor.title,
                    collaborationRevision: async () => {
                      if (!(await editor.saveNow())) throw new Error("请先同步修改并处理冲突。");
                      return editor.getVersion();
                    },
                    transferred: () => {
                      discardAccess();
                      navigate(personalPath, { replace: true });
                    },
                  });
                }}
              >
                <Users />
              </ChromeButton>
            )}
            {role === "owner" && <CollaborationExport epoch={epoch} />}
            {canGenerate && (
              <ChromeTooltip label="小镜">
                <ChromeButton aria-label="小镜" onClick={toggle}>
                  <Sparkles />
                </ChromeButton>
              </ChromeTooltip>
            )}
            {conflicts.length > 0 && (
              <ChromeButton aria-label="处理协作冲突" onClick={() => setConflictsOpen(true)}>
                <CircleAlert />
              </ChromeButton>
            )}
            {role === "owner" ? (
              <ChromeButton
                className="bg-destructive/10 text-destructive hover:bg-destructive/20 hover:text-destructive"
                onClick={() => setModeOpen(true)}
              >
                关闭协作
              </ChromeButton>
            ) : (
              <CollaborationExit
                onConfirm={async () => {
                  await canvasSharing.remove(editor.canvasId, "me");
                  discardAccess();
                  navigate(sharedPath, { replace: true });
                }}
              />
            )}
          </ChromePill>
        }
      />
      <CanvasModeDialog
        canvasId={editor.canvasId}
        active={false}
        open={role === "owner" && modeOpen}
        onOpenChange={setModeOpen}
        beforeConfirm={editor.saveNow}
        onChanged={() => {
          discardAccess();
          navigate(`/canvas/${editor.canvasId}`, { replace: true });
        }}
      />
      <Dialog open={historyOpen} onOpenChange={setHistoryOpen}>
        <DialogContent
          surface="glass"
          className="flex max-h-[min(88svh,720px)] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl"
        >
          <DialogHeader className="border-chrome-border shrink-0 border-b px-6 py-5 pr-14">
            <DialogTitle className="text-lg font-semibold tracking-tight">版本活动</DialogTitle>
          </DialogHeader>
          <DialogSession open={historyOpen}>
            <CanvasVersionHistory
              canvasId={editor.canvasId}
              epoch={epoch}
              openCopy={
                role === "owner" ? (id) => navigate(`/canvas/${encodeURIComponent(id)}`) : undefined
              }
              locate={(id) => {
                const located = editor.locateNode(id);
                if (located) setHistoryOpen(false);
                return located;
              }}
            />
          </DialogSession>
        </DialogContent>
      </Dialog>
      <Dialog
        open={commentNode !== null}
        onOpenChange={(open) => {
          if (!open) setCommentNode(null);
        }}
      >
        <DialogContent surface="glass">
          <DialogHeader>
            <DialogTitle>节点评论</DialogTitle>
            <DialogDescription>向协作成员交接这个节点。</DialogDescription>
          </DialogHeader>
          {commentNode && (
            <CanvasCommentThread
              key={commentNode}
              canvasId={editor.canvasId}
              nodeId={commentNode}
              epoch={epoch}
            />
          )}
        </DialogContent>
      </Dialog>
      <Dialog
        open={undoResult !== null}
        onOpenChange={(open) => {
          if (!open) dismissUndo();
        }}
      >
        <DialogContent surface="glass">
          <DialogHeader>
            <DialogTitle>{undoResult?.redo ? "个人重做" : "个人撤销"}</DialogTitle>
            <DialogDescription>
              {undoResult?.redo
                ? undoResult.undoes
                  ? "已检查你最近撤销的编辑。"
                  : "当前没有可重做的编辑。"
                : undoResult?.undoes
                  ? "已检查你最近确认的一笔编辑。"
                  : "当前没有可撤销的编辑。"}
            </DialogDescription>
          </DialogHeader>
          {undoResult?.undoes && (
            <div className="max-h-52 space-y-2 overflow-auto text-sm">
              {undoResult.changes?.flatMap((change) =>
                (change.action
                  ? [change.action === "create" ? "恢复对象" : "删除对象"]
                  : Object.keys(change.fields ?? {})
                ).map((field) => (
                  <p key={`${change.kind}:${change.id}:${field}`}>
                    {undoResult.redo ? "已重做" : "已撤销"} · {field} · {change.id}
                  </p>
                )),
              )}
              {undoResult.skipped?.map((item) => (
                <p key={`${item.kind}:${item.id}:${item.field}`}>
                  已保留 · {item.field || "结构变更"} · {item.id}
                </p>
              ))}
            </div>
          )}
        </DialogContent>
      </Dialog>
      <Dialog open={conflictsOpen} onOpenChange={setConflictsOpen}>
        <DialogContent surface="glass">
          <DialogHeader>
            <DialogTitle>处理协作冲突</DialogTitle>
            <DialogDescription>
              比较编辑起点、最新内容和你的草稿，再选择如何处理。
            </DialogDescription>
          </DialogHeader>
          <div className="max-h-[55vh] space-y-3 overflow-auto">
            {conflicts
              .filter((edit) => edit.expired || edit.rejected || edit.conflict?.length)
              .map(({ batch, conflict, expired, rejected, origins }) => (
                <section
                  key={batch.opId}
                  className="border-border grid gap-2 rounded-xl border p-3 text-sm"
                >
                  {rejected && <p>服务端未接受这笔草稿，{batch.changes.length} 项改动均未保存。</p>}
                  {expired && !rejected && (
                    <p>
                      这笔草稿已超过 24 小时，涉及 {batch.changes.length}{" "}
                      项改动。应用前请确认当前画布内容。
                    </p>
                  )}
                  {conflict?.map((item) => {
                    const draft = batch.changes.find((change) => change.id === item.id)?.fields?.[
                      item.field
                    ];
                    return (
                      <div key={`${item.id}:${item.field}`} className="grid gap-1 break-all">
                        <strong>{item.field || "节点或连线"}</strong>
                        {item.kind !== "edge" &&
                          editor.nodes.some((node) => node.id === item.id) && (
                            <Button
                              variant="ghost"
                              size="sm"
                              className="w-fit"
                              onClick={() => {
                                setConflictsOpen(false);
                                editor.locateNode(item.id);
                              }}
                            >
                              定位节点
                            </Button>
                          )}
                        {item.field && (
                          <span>共同起点　{formatDraft(origins?.[item.id]?.[item.field])}</span>
                        )}
                        <span>最新内容　{formatDraft(item.value)}</span>
                        <span>我的草稿　{formatDraft(draft)}</span>
                      </div>
                    );
                  })}
                  <div className="flex flex-wrap gap-2">
                    <Button variant="outline" onClick={() => resolve(batch.opId, false)}>
                      保留最新内容
                    </Button>
                    <Button onClick={() => resolve(batch.opId, true)}>应用我的草稿</Button>
                  </div>
                </section>
              ))}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}

function formatDraft(value: unknown) {
  return typeof value === "string" ? value : (JSON.stringify(value) ?? "无");
}

function CollaborationExport({ epoch }: { epoch: CanvasVersion }) {
  const editor = useCanvasEditor();
  const { exporting, cancelling, startExport, cancelExport } = useCanvasExport({
    canvasId: editor.canvasId,
    title: editor.title,
    saveNow: editor.saveNow,
    getVersion: () => ({ epoch, revision: editor.getVersion() }),
  });
  return (
    <>
      <ChromeTooltip label="导出画布" side="bottom">
        <ChromeButton aria-label="导出画布" disabled={exporting} onClick={() => void startExport()}>
          <FileDown />
        </ChromeButton>
      </ChromeTooltip>
      <TransferProgressDialog
        open={exporting}
        kind="export"
        cancelling={cancelling}
        onCancel={cancelExport}
      />
    </>
  );
}

export function CollaborationExit({ onConfirm }: { onConfirm: () => Promise<void> }) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const confirm = async () => {
    setBusy(true);
    setError(null);
    try {
      await onConfirm();
      setOpen(false);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "操作未完成");
    } finally {
      setBusy(false);
    }
  };
  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (!busy) {
          setError(null);
          setOpen(next);
        }
      }}
    >
      <AlertDialogTrigger
        render={
          <ChromeButton className="bg-destructive/10 text-destructive hover:bg-destructive/20 hover:text-destructive">
            退出协作
          </ChromeButton>
        }
      />
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>退出这张画布的协作？</AlertDialogTitle>
          <AlertDialogDescription>
            退出后，你将失去编辑和查看权限，画布也会从「共享给我」移除。已同步的修改会保留，未同步的本地草稿会清除。需要再次加入时，请联系所有者重新邀请。
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error && (
          <p role="alert" className="text-destructive text-sm">
            {error}
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy}>取消</AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={busy} onClick={() => void confirm()}>
            {busy ? "正在处理…" : "确认退出协作"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
