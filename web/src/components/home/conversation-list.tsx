import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { MessageSquare, MoreHorizontal, Pencil, Plus, Trash2 } from "lucide-react";

import type { ConversationDto } from "@/api/conversation/type";
import { ConfirmDialog } from "@/components/admin-ui/confirm-dialog";
import { ListItem, ListSection } from "@/components/home/sidebar-list";
import { RenameDialog } from "@/components/home/rename-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Skeleton } from "@/components/ui/skeleton";
import { useConversationRecordsStore } from "@/store/conversation-records";
import { useConversationsStore } from "@/store/conversations";
import { useTasksStore } from "@/store/tasks";
import { isActiveStatus } from "@/utils/tasks/status";

/** 侧栏最多列几段对话，按最近使用排序，更多的滚动查看 */
const LIST_LIMIT = 20;

/** 对话任务的 node_id 前缀（rec:{记录id}:{格子序号}），用来在任务库里认出它们 */
const RECORD_NODE_PREFIX = "rec:";

/**
 * 侧栏「对话」分组：所有对话一视同仁，按最近记录排序；标题右侧的 + 回到创作页，在那里发送即新建一段对话。
 * 有进行中生成的对话右侧亮一个小圆点；每项悬停出现「⋯」：重命名、删除。
 * 进行中的生成全部结束时重新拉一次列表，圆点才会跟着熄灭。
 * @param pathname 当前路径，用来高亮当前对话
 */
export function ConversationList({ pathname }: { pathname: string }) {
  const navigate = useNavigate();
  const items = useConversationsStore((state) => state.items);
  const status = useConversationsStore((state) => state.status);
  const load = useConversationsStore((state) => state.load);
  const [renaming, setRenaming] = useState<ConversationDto | null>(null);
  const [deleting, setDeleting] = useState<ConversationDto | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    void load();
  }, [load]);

  /** 对话里有没有进行中的任务：从有到无的那一刻刷新列表 */
  const hasActive = useTasksStore((state) =>
    Object.values(state.tasks).some(
      (task) => task.node_id.startsWith(RECORD_NODE_PREFIX) && isActiveStatus(task.status),
    ),
  );
  const wasActive = useRef(false);
  useEffect(() => {
    if (wasActive.current && !hasActive) void load();
    wasActive.current = hasActive;
  }, [hasActive, load]);

  const confirmDelete = async () => {
    if (!deleting) return;
    setBusy(true);
    try {
      await useConversationsStore.getState().remove(deleting.id);
      useConversationRecordsStore.getState().drop(deleting.id);
      if (pathname === `/conversations/${deleting.id}`) navigate("/", { replace: true });
      setDeleting(null);
    } catch {
      // 提示由请求层统一弹，确认框保持打开
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <ListSection
        title="对话"
        action={
          <button
            type="button"
            aria-label="新对话"
            title="新对话"
            onClick={() => navigate("/")}
            className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-ring/50 grid size-6 place-items-center rounded-md outline-none focus-visible:ring-2"
          >
            <Plus className="size-3.5" />
          </button>
        }
      >
        {status === "loading" && items.length === 0 && (
          <div className="grid gap-1 px-2.5" aria-busy>
            <Skeleton className="h-8" />
            <Skeleton className="h-8" />
          </div>
        )}
        {status === "error" && items.length === 0 && (
          <button
            type="button"
            onClick={() => void load()}
            className="text-muted-foreground hover:text-foreground px-2.5 py-1 text-left text-xs"
          >
            加载失败 · 重试
          </button>
        )}
        {items.slice(0, LIST_LIMIT).map((conversation) => (
          <ListItem
            key={conversation.id}
            to={`/conversations/${conversation.id}`}
            title={conversation.title}
            active={pathname === `/conversations/${conversation.id}`}
            thumb={<MessageSquare />}
            trailing={
              <div className="flex items-center gap-1">
                {conversation.active && (
                  <span
                    role="img"
                    aria-label="有生成进行中"
                    title="有生成进行中"
                    className="bg-beam size-1.5 animate-pulse rounded-full group-focus-within/item:hidden group-hover/item:hidden"
                  />
                )}
                <DropdownMenu modal={false}>
                  <DropdownMenuTrigger
                    aria-label={`「${conversation.title}」的操作`}
                    className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-ring/50 grid size-6 place-items-center rounded-md opacity-0 outline-none group-focus-within/item:opacity-100 group-hover/item:opacity-100 focus-visible:ring-2 data-popup-open:opacity-100"
                  >
                    <MoreHorizontal className="size-4" />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="start" side="right" sideOffset={6} className="w-40">
                    <DropdownMenuItem onClick={() => setRenaming(conversation)}>
                      <Pencil />
                      重命名
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      variant="destructive"
                      onClick={() => setDeleting(conversation)}
                    >
                      <Trash2 />
                      删除
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              </div>
            }
          />
        ))}
      </ListSection>

      <RenameDialog
        open={renaming !== null}
        title={renaming?.title ?? ""}
        onClose={() => setRenaming(null)}
        onSubmit={(title) => useConversationsStore.getState().rename(renaming!.id, title)}
      />
      <ConfirmDialog
        open={deleting !== null}
        title={`删除「${deleting?.title ?? ""}」？`}
        description="对话里的记录会一起删除；进行中的生成会继续跑完，生成的素材仍保留在资产里。"
        confirmLabel="删除"
        destructive
        busy={busy}
        onConfirm={() => void confirmDelete()}
        onCancel={() => setDeleting(null)}
      />
    </>
  );
}
