import { Upload } from "lucide-react";
import { MotionConfig } from "motion/react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useDropzone } from "react-dropzone";
import { toast } from "sonner";

import type { SkillItem } from "@/api/admin/agent-skill/type.d";
import { DataTablePagination } from "@/components/admin-ui/data-table-pagination";
import { ListSearch } from "@/components/admin-ui/list-search";
import { Notice } from "@/components/admin-ui/notice";
import {
  PageHeader,
  PageHeaderActions,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { importedMessage, type SkillListFilter } from "@/utils/admin/agent-skill";

import { DropOverlay } from "./drop-overlay";
import { getFilesFromEvent, toPicked } from "./drop-files";
import { ImportDialog } from "./import-dialog";
import { SkillDialog } from "./skill-dialog";
import { SkillTable } from "./skill-table";
import { useAgentSkills } from "./use-agent-skills";
import { useSkillImport, type ImportedResult } from "./use-skill-import";

const FILTERS: { id: SkillListFilter; label: string }[] = [
  { id: "all", label: "全部" },
  { id: "enabled", label: "已启用" },
  { id: "disabled", label: "已停用" },
  { id: "builtin", label: "内置" },
];

/**
 * Agent 技能管理页：列表 + 工具栏 + 详情弹窗 + 居中导入对话框。
 * 页面根节点是 react-dropzone 的拖放区（noClick），文件拖到页面任意位置都会打开导入对话框并直接上传；
 * 对话框开着时由第 1 步的拖放区高亮。按 / 聚焦搜索。
 */
export default function AgentSkillsPage() {
  const api = useAgentSkills();
  const [openSkill, setOpenSkill] = useState<SkillItem | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);

  const onImported = useCallback(
    ({ item, view }: ImportedResult) => {
      setDialogOpen(false);
      api.markFresh(item.name);
      toast.success(importedMessage(view));
      api.reload();
    },
    [api],
  );
  const importer = useSkillImport(onImported);

  const openImport = () => {
    importer.reset();
    setDialogOpen(true);
  };
  const closeImport = () => {
    if (importer.close()) setDialogOpen(false);
  };

  const { getRootProps, isDragActive } = useDropzone({
    noClick: true,
    noKeyboard: true,
    noPaste: true,
    multiple: true,
    getFilesFromEvent,
    onDrop: (files) => {
      if (files.length === 0) return;
      if (!dialogOpen) {
        importer.reset();
        setDialogOpen(true);
      }
      void importer.begin(toPicked(files));
    },
    onError: () => toast.error("读取拖入的文件失败，请改用“选择文件夹”或压缩包"),
  });

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      if (event.key !== "/" || target?.closest("input, textarea, [contenteditable]")) return;
      event.preventDefault();
      searchRef.current?.focus();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);


  return (
    <MotionConfig reducedMotion="user">
      <div {...getRootProps({ className: "h-full overflow-y-auto" })}>
        <main className="px-4 py-6 lg:px-6">
          <PageHeader>
            <PageHeaderHeading>
              <PageHeaderTitle>技能</PageHeaderTitle>
            </PageHeaderHeading>
            <PageHeaderActions>
              <Button onClick={openImport}>
                <Upload />
                导入技能
              </Button>
            </PageHeaderActions>
          </PageHeader>

          <div className="mb-3 flex flex-wrap items-center gap-2">
            <ListSearch
              ref={searchRef}
              className="w-60 max-md:w-full"
              placeholder="搜索名称或说明（/）"
              value={api.q}
              onCommit={api.setQ}
            />
            <Segmented aria-label="状态筛选">
              {FILTERS.map((f) => (
                <SegmentedItem
                  key={f.id}
                  slideId="skill-filter"
                  active={(api.filters.status ?? "all") === f.id}
                  onClick={() => api.setFilter("status", f.id)}
                >
                  {f.label}
                </SegmentedItem>
              ))}
            </Segmented>
            <span className="text-muted-foreground ml-auto text-xs tabular-nums">
              共 {api.total} 个
            </span>
          </div>

          {api.status === "error" ? (
            <Notice
              tone="danger"
              title="技能列表加载失败"
              action={
                <Button variant="outline" size="sm" onClick={() => void api.reload()}>
                  重试
                </Button>
              }
            />
          ) : (
            <div className={cn(api.refreshing && "opacity-60")}>
              <SkillTable
                items={api.items}
                loading={api.status === "loading"}
                hasImported={api.hasImported}
                busyNames={api.busyNames}
                freshName={api.freshName}
                onOpen={(item) => setOpenSkill(item)}
                onToggle={(item, enabled) => void api.toggleEnabled(item, enabled)}
                onImport={openImport}
              />
              <div className="mt-3 border-t py-3">
                <DataTablePagination
                  page={api.page}
                  pageSize={api.pageSize}
                  total={api.total}
                  onPageChange={api.setPage}
                  onPageSizeChange={api.setPageSize}
                />
              </div>
            </div>
          )}
        </main>

        <SkillDialog
          target={openSkill}
          api={api}
          onClose={() => {
            setOpenSkill(null);
            api.reload();
          }}
        />
        <ImportDialog
          open={dialogOpen}
          importer={importer}
          dragActive={isDragActive}
          onClose={closeImport}
          onExited={importer.reset}
        />
        <DropOverlay active={isDragActive && !dialogOpen} />
      </div>
    </MotionConfig>
  );
}
