import { useState } from "react";
import { AnimatePresence } from "motion/react";
import { Search } from "lucide-react";

import {
  CanvasCard,
  CanvasCardSkeleton,
  CardListNote,
  NewCanvasCard,
} from "@/components/home/canvas-card";
import { ImportCanvasButton } from "@/components/home/import-canvas-button";
import { ImportResultDialog } from "@/components/home/import-result-dialog";
import { Segmented } from "@/components/home/segmented";
import { Button } from "@/components/ui/button";
import { RECENT_CANVAS_LIMIT } from "@/constants/creation";
import { useCanvasImport } from "@/hooks/use-canvas-import";
import { useCanvasList } from "@/hooks/use-canvas-list";
import { cn } from "@/lib/utils";

/** 还没有分页控件，先一次拉够 */
const PAGE_SIZE = 60;

/** 网格列数；窄屏最多 2 列 */
const GRID = "grid-cols-2 md:grid-cols-4 xl:grid-cols-5";

type Scope = "all" | "mine" | "shared";

/**
 * 所有画布（设计稿 docs/design/首页 4.2）：tabs + 工具条 + 画布网格。
 * 搜索是真功能；协作画布先占位。文件夹、多选、分享链接、疏密切换和排序都去掉了（没有对应功能）。
 */
export default function CanvasList() {
  const [scope, setScope] = useState<Scope>("mine");
  const list = useCanvasList(PAGE_SIZE);
  const canvasImport = useCanvasImport(list.reload);

  let body;
  if (list.loading) {
    body = Array.from({ length: 7 }, (_, i) => <CanvasCardSkeleton key={i} variant="panel" />);
  } else if (list.error) {
    body = (
      <CardListNote key="note">
        加载失败
        <Button variant="outline" size="sm" onClick={list.reload}>
          重试
        </Button>
      </CardListNote>
    );
  } else if (list.items.length === 0) {
    body = (
      <CardListNote key="note">
        {list.keyword ? (
          <>
            没有找到“{list.keyword}”
            <Button variant="link" size="sm" onClick={() => list.setQuery("")}>
              清空搜索
            </Button>
          </>
        ) : (
          "还没有画布，从这里开始"
        )}
      </CardListNote>
    );
  } else {
    body = list.items.map((canvas, index) => (
      <CanvasCard
        key={canvas.id}
        canvas={canvas}
        variant="panel"
        shared={index < RECENT_CANVAS_LIMIT}
        deleting={list.deleting.has(canvas.id)}
        onDelete={() => void list.remove(canvas.id)}
      />
    ));
  }

  return (
    <div className="w-full px-4 pt-2 pb-24 md:px-6">
      <div className="mb-5 flex flex-wrap items-center gap-2">
        <Segmented<Scope>
          size="md"
          value={scope}
          onChange={setScope}
          options={[
            { value: "all", label: "全部" },
            { value: "mine", label: "个人画布" },
            { value: "shared", label: "协作画布", soon: true },
          ]}
        />
        <div className="flex-1" />
        <ImportCanvasButton importing={canvasImport.importing} onPick={canvasImport.pick} />
        <label className="bg-muted/60 text-muted-foreground focus-within:ring-ring flex h-9 w-36 items-center gap-2 rounded-[10px] px-3 transition-shadow focus-within:ring-1 md:w-56">
          <Search className="size-4 shrink-0" />
          <input
            value={list.query}
            onChange={(event) => list.setQuery(event.target.value)}
            onKeyDown={(event) => event.key === "Escape" && list.setQuery("")}
            placeholder="搜索"
            aria-label="搜索画布"
            className="text-foreground min-w-0 flex-1 bg-transparent outline-none"
          />
        </label>
      </div>

      <div className={cn("grid gap-2.5 md:gap-4", GRID)}>
        <NewCanvasCard
          variant="panel"
          creating={list.creating}
          onCreate={() => void list.create()}
        />
        <AnimatePresence initial={false}>{body}</AnimatePresence>
      </div>

      <ImportResultDialog
        report={canvasImport.pending?.report ?? null}
        onClose={canvasImport.closeReport}
        onOpen={canvasImport.openImported}
      />
    </div>
  );
}
