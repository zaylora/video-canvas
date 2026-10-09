import { Fragment, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { Loader2, MoreHorizontal, Pencil, Trash2 } from "lucide-react";

import { ConfirmDialog } from "@/components/admin-ui/confirm-dialog";
import { RecordView } from "@/components/conversation/record-view";
import { Composer } from "@/components/home/composer";
import { EmptyMark } from "@/components/home/empty-mark";
import { RenameDialog } from "@/components/home/rename-dialog";
import { Skeleton } from "@/components/ui/skeleton";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { addResultAsReference } from "@/hooks/use-record-actions";
import { useConversationRecordsStore } from "@/store/conversation-records";
import { useConversationsStore } from "@/store/conversations";
import { dayTitle } from "@/utils/conversation/day";

/**
 * 对话页：生成记录按日期分组、时间正序，打开时停在最新一条；输入卡片固定在页面底部，
 * 和创作页共用同一份模式、模型、参数与草稿。在这里发送会追加到当前对话。
 * 记录的结果状态来自任务库，断线重连后由对账补齐。
 */
export default function ConversationPage() {
  const { id = "" } = useParams();
  const navigate = useNavigate();

  const conversation = useConversationsStore((state) => state.items.find((item) => item.id === id));
  const loadConversations = useConversationsStore((state) => state.load);
  const entry = useConversationRecordsStore((state) => state.byId[id]);
  const loadRecords = useConversationRecordsStore((state) => state.load);
  const loadMore = useConversationRecordsStore((state) => state.loadMore);

  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    void loadConversations();
  }, [loadConversations]);

  /** 进入或切换对话时加载最新一页；已有缓存先显示缓存 */
  useEffect(() => {
    if (id) void loadRecords(id);
  }, [id, loadRecords]);

  const items = useMemo(() => entry?.items ?? [], [entry]);

  /** 首屏加载完成直接定位到最新一条，不做滚动动画；之后有新记录再平滑滚到底 */
  const ready = entry?.status === "ready";
  const lastCount = useRef(0);
  useEffect(() => {
    if (!ready) return;
    const first = lastCount.current === 0;
    if (first || items.length > lastCount.current) {
      window.scrollTo({ top: document.body.scrollHeight, behavior: first ? "instant" : "smooth" });
    }
    lastCount.current = items.length;
  }, [ready, items.length]);
  /** 换对话后重新按「首屏」处理 */
  useEffect(() => {
    lastCount.current = 0;
  }, [id]);

  /**
   * 记录加载失败：对话被删除（62001）或网络异常。不能靠「列表里没有它」判断——
   * 刚在新对话页发送成功时列表还没刷新，会闪一下「不存在」。
   */
  const failed = entry?.status === "error";
  const title = conversation?.title ?? "";

  const confirmDelete = async () => {
    if (!conversation) return;
    setBusy(true);
    try {
      await useConversationsStore.getState().remove(conversation.id);
      useConversationRecordsStore.getState().drop(conversation.id);
      setDeleting(false);
      navigate("/", { replace: true });
    } catch {
      // 提示由请求层统一弹，确认框保持打开
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto flex min-h-[calc(100svh-3.5rem)] w-full max-w-260 flex-col px-4 md:px-6">
      {!failed && (
        <header className="flex h-10 items-center gap-2">
          <h1 className="min-w-0 truncate text-sm font-medium">{title}</h1>
          {conversation && (
            <DropdownMenu modal={false}>
              <DropdownMenuTrigger
                aria-label="对话操作"
                className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring/50 grid size-7 place-items-center rounded-lg outline-none focus-visible:ring-3"
              >
                <MoreHorizontal className="size-4" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="start" sideOffset={6} className="w-40">
                <DropdownMenuItem onClick={() => setRenaming(true)}>
                  <Pencil />
                  重命名
                </DropdownMenuItem>
                <DropdownMenuItem variant="destructive" onClick={() => setDeleting(true)}>
                  <Trash2 />
                  删除对话
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </header>
      )}

      <div className="flex-1 pb-2" aria-live="polite">
        {failed ? (
          <EmptyMark
            title="对话加载失败"
            hint={
              <span className="inline-flex items-center gap-2">
                它可能已被删除，或网络异常
                <button
                  type="button"
                  onClick={() => void loadRecords(id)}
                  className="text-foreground underline underline-offset-2"
                >
                  重试
                </button>
              </span>
            }
          />
        ) : !entry || entry.status === "loading" ? (
          <div className="grid gap-6 pt-2" aria-busy>
            <Skeleton className="h-6 w-2/3" />
            <Skeleton className="h-60 w-96 max-w-full" />
          </div>
        ) : items.length === 0 ? (
          <EmptyMark title="还没有记录" hint="在下面输入想法开始" />
        ) : (
          <>
            {entry.next && (
              <div className="mb-4 flex justify-center">
                <button
                  type="button"
                  disabled={entry.loadingMore}
                  onClick={() => void loadMore(id)}
                  className="text-muted-foreground hover:text-foreground inline-flex h-8 items-center gap-1.5 rounded-lg px-3 text-[13px] disabled:opacity-60"
                >
                  {entry.loadingMore && <Loader2 className="size-3.5 animate-spin" />}
                  加载更早的记录
                </button>
              </div>
            )}
            {items.map((record, index) => {
              const day = dayTitle(record.createdAt);
              const newDay = index === 0 || dayTitle(items[index - 1].createdAt) !== day;
              return (
                <Fragment key={record.id}>
                  {newDay && (
                    <h2 className="mt-1 mb-4.5 text-[22px] font-semibold tracking-tight">{day}</h2>
                  )}
                  <RecordView
                    record={record}
                    conversationId={id}
                    onUseAsRef={addResultAsReference}
                  />
                </Fragment>
              );
            })}
          </>
        )}
      </div>

      <div className="from-background via-background sticky bottom-0 z-10 bg-linear-to-t via-70% to-transparent pt-4 pb-5">
        <Composer placement="dock" target={id} />
      </div>

      {conversation && (
        <RenameDialog
          open={renaming}
          title={conversation.title}
          onClose={() => setRenaming(false)}
          onSubmit={(next) => useConversationsStore.getState().rename(conversation.id, next)}
        />
      )}
      <ConfirmDialog
        open={deleting}
        title={`删除「${conversation?.title ?? ""}」？`}
        description="对话里的记录会一起删除；进行中的生成会继续跑完，生成的素材仍保留在资产里。"
        confirmLabel="删除"
        destructive
        busy={busy}
        onConfirm={() => void confirmDelete()}
        onCancel={() => setDeleting(false)}
      />
    </div>
  );
}
