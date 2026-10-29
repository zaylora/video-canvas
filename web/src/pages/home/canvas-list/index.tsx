import { Link, useSearchParams } from "react-router";
import { Plus } from "lucide-react";

import type { CanvasListItemDto } from "@/api/canvas/type";
import type { DramaSummary } from "@/api/drama/type";
import { AssetSearch } from "@/components/assets/asset-search";
import { AssetsToolbar } from "@/components/assets/assets-toolbar";
import { TransferProgressDialog } from "@/components/canvas/transfer-progress-dialog";
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
import { useDramaList } from "@/hooks/use-drama-list";
import { DramaCard } from "@/pages/drama/drama-grid";
import { mixWorks, parseWorksScope, type WorksScope } from "@/utils/home/works";

/** 画布还没有分页控件，先一次拉够 */
const PAGE_SIZE = 60;
/** 短剧每次加载的条数 */
const DRAMA_PAGE_SIZE = 24;

/** 作品网格：和短剧工作室首页的短剧网格同一套列数和间距 */
const GRID = "grid grid-cols-2 gap-x-4 gap-y-7 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5";

/** 「＋ 新建短剧」卡：和画布的新建卡同一个样子，点了去短剧工作室首页新建 */
function NewDramaCard() {
  return (
    <Link
      to="/drama"
      data-slot="new-drama-card"
      className="group/new text-muted-foreground hover:text-foreground focus-visible:ring-ring/60 relative block min-w-0 rounded-[18px] transition-colors duration-150 outline-none focus-visible:ring-3"
    >
      <span className="group-hover/new:bg-chrome-hover flex aspect-16/10 flex-col items-center justify-center gap-2.5 rounded-[18px] border border-dashed border-[color-mix(in_oklch,var(--foreground)_18%,transparent)] transition-colors duration-150">
        <span className="bg-chrome-hover ring-chrome-border grid size-12 place-items-center rounded-full ring-1">
          <Plus className="size-5" />
        </span>
        <span className="text-sm">新建短剧</span>
      </span>
    </Link>
  );
}

/**
 * 我的作品（原「所有画布」，设计稿 docs/design/首页 4.2）：大标题 + 半透明工具条（范围、导入画布、搜索）+ 作品网格。
 * 范围写在地址栏 ?scope=：全部（画布和短剧按更新时间混排）、个人画布（默认）、个人短剧；
 * 短剧工作室首页的「更多」进来就是 ?scope=drama。搜索同时作用于画布和短剧。
 */
export default function CanvasList() {
  const [params, setParams] = useSearchParams();
  const scope = parseWorksScope(params.get("scope"));
  const list = useCanvasList(PAGE_SIZE);
  const dramas = useDramaList(DRAMA_PAGE_SIZE, list.keyword);
  const canvasImport = useCanvasImport(list.reload);

  const setScope = (next: WorksScope) =>
    setParams(next === "mine" ? {} : { scope: next }, { replace: true });

  const canvasCard = (canvas: CanvasListItemDto, index: number) => (
    <CanvasCard
      key={`canvas-${canvas.id}`}
      canvas={canvas}
      variant="panel"
      shared={index < RECENT_CANVAS_LIMIT}
      deleting={list.deleting.has(canvas.id)}
      onDelete={() => void list.remove(canvas.id)}
    />
  );
  const dramaCard = (drama: DramaSummary, index: number) => (
    <DramaCard
      key={`drama-${drama.id}`}
      drama={drama}
      index={index}
      instant
      deleting={dramas.deletingId === drama.id}
      onDelete={() => dramas.remove(drama.id)}
    />
  );

  const showCanvases = scope !== "drama";
  const showDramas = scope !== "mine";
  const loading =
    (showCanvases && list.loading) || (showDramas && dramas.loading && dramas.items.length === 0);
  const failed =
    (showCanvases && list.error) || (showDramas && dramas.failed && dramas.items.length === 0);
  const empty =
    (!showCanvases || list.items.length === 0) && (!showDramas || dramas.items.length === 0);

  let body;
  if (loading) {
    body = Array.from({ length: 7 }, (_, i) => <CanvasCardSkeleton key={i} variant="panel" />);
  } else if (failed) {
    body = (
      <CardListNote key="note">
        加载失败
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            if (showCanvases) list.reload();
            if (showDramas) dramas.reload();
          }}
        >
          重试
        </Button>
      </CardListNote>
    );
  } else if (empty) {
    body = (
      <CardListNote key="note">
        {list.keyword ? (
          <>
            没有找到“{list.keyword}”
            <Button variant="link" size="sm" onClick={() => list.setQuery("")}>
              清空搜索
            </Button>
          </>
        ) : scope === "drama" ? (
          "还没有短剧"
        ) : (
          "还没有作品，从这里开始"
        )}
      </CardListNote>
    );
  } else if (scope === "mine") {
    body = list.items.map(canvasCard);
  } else if (scope === "drama") {
    body = dramas.items.map(dramaCard);
  } else {
    body = mixWorks(list.items, dramas.items).map((work, index) =>
      work.kind === "canvas" ? canvasCard(work.item, index) : dramaCard(work.item, index),
    );
  }

  return (
    <div className="mx-auto w-full max-w-320 px-4 pb-24 md:px-10">
      <h1 className="pt-6 text-[28px] font-semibold tracking-[-0.025em] md:pt-10 md:text-[34px]">
        我的作品
      </h1>

      <AssetsToolbar className="mt-6 mb-7">
        <Segmented<WorksScope>
          variant="chrome"
          label="作品范围"
          value={scope}
          onChange={setScope}
          options={[
            { value: "all", label: "全部" },
            { value: "mine", label: "个人画布" },
            { value: "drama", label: "个人短剧" },
          ]}
        />
        <div className="ml-auto flex items-center gap-1">
          {showCanvases && (
            <ImportCanvasButton importing={canvasImport.importing} onPick={canvasImport.pick} />
          )}
          <AssetSearch value={list.query} onChange={list.setQuery} placeholder="搜索" />
        </div>
      </AssetsToolbar>

      <div className={GRID}>
        {scope === "drama" ? (
          <NewDramaCard />
        ) : (
          <NewCanvasCard
            variant="panel"
            creating={list.creating}
            onCreate={() => void list.create()}
          />
        )}
        {/* 切换范围时直接换内容，不做列表的进出场动画 */}
        {body}
      </div>

      {showDramas && dramas.hasMore && !loading && (
        <div className="mt-8 flex justify-center">
          <Button
            variant="outline"
            className="rounded-full"
            disabled={dramas.loading}
            onClick={dramas.loadMore}
          >
            {dramas.loading ? "加载中…" : "加载更多短剧"}
          </Button>
        </div>
      )}

      <ImportResultDialog
        report={canvasImport.pending?.report ?? null}
        onClose={canvasImport.closeReport}
        onOpen={canvasImport.openImported}
      />
      {/* 导入期间把用户锁在进度弹窗里，不能点列表、不能再选文件 */}
      <TransferProgressDialog
        open={canvasImport.importing}
        kind="import"
        cancelling={canvasImport.cancelling}
        onCancel={canvasImport.cancel}
      />
    </div>
  );
}
