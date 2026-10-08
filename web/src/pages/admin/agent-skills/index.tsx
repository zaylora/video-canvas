import { Upload } from "lucide-react";
import { MotionConfig } from "motion/react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useDropzone } from "react-dropzone";
import { toast } from "sonner";

import { Notice } from "@/components/admin-ui/notice";
import {
  PageHeader,
  PageHeaderActions,
  PageHeaderDescription,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { SearchInput } from "@/components/admin-ui/search-input";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { Button } from "@/components/ui/button";
import { filterSkillItems, importedMessage, type SkillListFilter } from "@/utils/admin/agent-skill";

import { DropOverlay } from "./drop-overlay";
import { getFilesFromEvent, toPicked } from "./drop-files";
import { ImportDialog } from "./import-dialog";
import { SkillSheet } from "./skill-sheet";
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
 * Agent 技能管理页：列表 + 工具栏 + 右侧详情抽屉 + 居中导入对话框。
 * 页面根节点是 react-dropzone 的拖放区（noClick），文件拖到页面任意位置都会打开导入对话框并直接上传；
 * 对话框开着时由第 1 步的拖放区高亮。按 / 聚焦搜索。
 */
export default function AgentSkillsPage() {
  const api = useAgentSkills();
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<SkillListFilter>("all");
  const [openName, setOpenName] = useState<string | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);

  const onImported = useCallback(
    ({ item, view }: ImportedResult) => {
      api.replaceOne(item);
      api.markFresh(item.name);
      toast.success(importedMessage(view));
      setDialogOpen(false);
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

  const visible = useMemo(
    () => filterSkillItems(api.items, query, filter),
    [api.items, query, filter],
  );
  const hasImported = api.items.some((item) => item.source === "imported");

  return (
    <MotionConfig reducedMotion="user">
      <div {...getRootProps({ className: "h-full overflow-y-auto" })}>
        <main className="px-4 py-6 lg:px-6">
          <PageHeader>
            <PageHeaderHeading>
              <PageHeaderTitle>技能</PageHeaderTitle>
              <PageHeaderDescription>
                管理画布 Agent 能用的技能。导入后默认停用；启用后，新开始的运行才会用到它。
              </PageHeaderDescription>
            </PageHeaderHeading>
            <PageHeaderActions>
              <Button onClick={openImport}>
                <Upload />
                导入技能
              </Button>
            </PageHeaderActions>
          </PageHeader>

          <div className="mb-3 flex flex-wrap items-center gap-2">
            <SearchInput
              ref={searchRef}
              className="w-60 max-md:w-full"
              placeholder="搜索名称或说明（/）"
              aria-label="搜索技能"
              value={query}
              onChange={(event) => setQuery(event.target.value)}
            />
            <Segmented aria-label="状态筛选">
              {FILTERS.map((f) => (
                <SegmentedItem
                  key={f.id}
                  slideId="skill-filter"
                  active={filter === f.id}
                  onClick={() => setFilter(f.id)}
                >
                  {f.label}
                </SegmentedItem>
              ))}
            </Segmented>
            <span className="text-muted-foreground ml-auto text-xs tabular-nums">
              共 {visible.length} 个
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
            <SkillTable
              items={visible}
              loading={api.status === "loading"}
              hasImported={hasImported}
              busyNames={api.busyNames}
              freshName={api.freshName}
              onOpen={(item) => setOpenName(item.name)}
              onToggle={(item, enabled) => void api.toggleEnabled(item, enabled)}
              onImport={openImport}
            />
          )}
        </main>

        <SkillSheet name={openName} api={api} onClose={() => setOpenName(null)} />
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
