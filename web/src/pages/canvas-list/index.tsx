import { useState } from "react";
import { AnimatePresence } from "motion/react";
import {
  CheckSquare,
  FolderPlus,
  Grid2x2,
  Grid3x3,
  Link2,
  ListFilter,
  Search,
  Square,
} from "lucide-react";

import {
  CanvasCard,
  CanvasCardSkeleton,
  CardListNote,
  NewCanvasCard,
} from "@/components/home/canvas-card";
import { Segmented } from "@/components/home/segmented";
import { SoonTip } from "@/components/home/soon";
import { Button } from "@/components/ui/button";
import { useCanvasList } from "@/hooks/use-canvas-list";
import { cn } from "@/lib/utils";

/** 还没有分页控件，先一次拉够 */
const PAGE_SIZE = 60;

/** 网格疏密：小 / 中 / 大，选择记在本机 */
type Density = "sm" | "md" | "lg";

const DENSITY_KEY = "canvas-list-density";

/** 各档的列数；窄屏一律最多 2 列 */
const DENSITY_GRID: Record<Density, string> = {
  sm: "grid-cols-2 md:grid-cols-4 xl:grid-cols-6",
  md: "grid-cols-2 md:grid-cols-3 xl:grid-cols-4",
  lg: "grid-cols-2 lg:grid-cols-3",
};

function readDensity(): Density {
  try {
    const value = localStorage.getItem(DENSITY_KEY);
    return value === "sm" || value === "lg" ? value : "md";
  } catch {
    return "md";
  }
}

type Scope = "all" | "mine" | "shared";

/** 工具条上还没有功能的按钮：窄屏只留图标 */
function SoonAction({ icon: Icon, label }: { icon: typeof Search; label: string }) {
  return (
    <SoonTip>
      <Button disabled variant="ghost" aria-label={label} className="text-muted-foreground">
        <Icon />
        <span className="max-lg:hidden">{label}</span>
      </Button>
    </SoonTip>
  );
}

/**
 * 所有画布（设计稿 docs/design/首页 4.2）：tabs + 工具条 + 画布网格。
 * 搜索和疏密切换是真功能；文件夹、多选、分享链接、排序、协作画布先占位。
 */
export default function CanvasList() {
  const [scope, setScope] = useState<Scope>("mine");
  const [density, setDensity] = useState<Density>(readDensity);
  const list = useCanvasList(PAGE_SIZE);

  const changeDensity = (next: Density) => {
    setDensity(next);
    try {
      localStorage.setItem(DENSITY_KEY, next);
    } catch {
      /** 存不进去只是下次不记得 */
    }
  };

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
    body = list.items.map((canvas) => (
      <CanvasCard
        key={canvas.id}
        canvas={canvas}
        variant="panel"
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
        <div className="flex items-center gap-1">
          <SoonAction icon={FolderPlus} label="新建文件夹" />
          <SoonAction icon={CheckSquare} label="多选" />
          <SoonAction icon={Link2} label="打开分享链接" />
        </div>
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
        <Segmented<Density>
          className="max-md:hidden"
          value={density}
          onChange={changeDensity}
          options={[
            { value: "sm", label: <Grid3x3 />, ariaLabel: "小图" },
            { value: "md", label: <Grid2x2 />, ariaLabel: "中图" },
            { value: "lg", label: <Square />, ariaLabel: "大图" },
          ]}
        />
        <SoonTip>
          <Button
            disabled
            variant="ghost"
            size="icon"
            aria-label="排序"
            className="text-muted-foreground"
          >
            <ListFilter />
          </Button>
        </SoonTip>
      </div>

      <div className={cn("grid gap-2.5 md:gap-4", DENSITY_GRID[density])}>
        <NewCanvasCard
          variant="panel"
          creating={list.creating}
          onCreate={() => void list.create()}
        />
        <AnimatePresence initial={false}>{body}</AnimatePresence>
      </div>
    </div>
  );
}
