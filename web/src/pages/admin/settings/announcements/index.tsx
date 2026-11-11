import { useState } from "react";
import { Megaphone, Plus } from "lucide-react";

import type { AnnouncementAdminItem } from "@/api/admin/announcement/type";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import {
  EmptyState,
  EmptyStateActions,
  EmptyStateIcon,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import { GLASS_PANEL } from "@/components/admin-ui/glass";
import {
  PageHeader,
  PageHeaderActions,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

import { SettingsError } from "../settings-state";
import { AnnouncementList } from "./announcement-list";
import { AnnouncementDialog } from "./announcement-dialog";
import { useAnnouncementsAdmin } from "./use-announcements-admin";

/** 弹窗的状态：关闭、新建（item 为 null）、编辑某一条 */
type EditorState = { open: false } | { open: true; item: AnnouncementAdminItem | null };

/**
 * 首页公告（/admin/settings/announcements，设计稿 docs/design/公告/公告.md §6.2）：
 * 公告列表（拖动排序、启用开关、编辑、删除）+ 新建 / 编辑弹窗。admin 与 super_admin 都能改。
 * 请求错误的全局提示由拦截器弹，这里不重复。
 */
export default function AnnouncementsSettingsPage() {
  const admin = useAnnouncementsAdmin();
  const [editor, setEditor] = useState<EditorState>({ open: false });
  const { items, status } = admin;

  const openNew = () => setEditor({ open: true, item: null });
  const askRemove = (item: AnnouncementAdminItem) =>
    void confirm({
      title: "删除这条公告？",
      description: `「${item.title}」删除后用户端不再显示，操作审计里会留下记录。`,
      confirmLabel: "删除",
      destructive: true,
      onConfirm: () => admin.remove(item),
    });

  return (
    <div className="h-full overflow-y-auto">
      <main className="px-4 py-6 lg:px-6">
        <PageHeader>
          <PageHeaderHeading>
            <PageHeaderTitle>公告</PageHeaderTitle>
          </PageHeaderHeading>
          {status === "ready" && items.length > 0 && (
            <PageHeaderActions>
              <Button onClick={openNew}>
                <Plus />
                新建公告
              </Button>
            </PageHeaderActions>
          )}
        </PageHeader>

        {status === "loading" && <Skeleton className="h-60 w-full rounded-xl" />}
        {status === "error" && <SettingsError onRetry={() => void admin.reload()} />}
        {status === "ready" && (
          <Card className={cn(GLASS_PANEL, "gap-0 py-0")}>
            {items.length === 0 ? (
              <EmptyState className="m-4 border-0">
                <EmptyStateIcon>
                  <Megaphone />
                </EmptyStateIcon>
                <EmptyStateTitle>还没有公告</EmptyStateTitle>
                <EmptyStateActions>
                  <Button onClick={openNew}>
                    <Plus />
                    新建公告
                  </Button>
                </EmptyStateActions>
              </EmptyState>
            ) : (
              <AnnouncementList
                items={items}
                busyIds={admin.busyIds}
                onReorder={(next) => void admin.reorder(next).catch(() => undefined)}
                onToggle={(item) => void admin.toggleEnabled(item)}
                onEdit={(item) => setEditor({ open: true, item })}
                onRemove={askRemove}
              />
            )}
          </Card>
        )}

        <AnnouncementDialog
          open={editor.open}
          item={editor.open ? editor.item : null}
          onClose={() => setEditor({ open: false })}
          onSaved={() => void admin.reload()}
        />
      </main>
    </div>
  );
}
