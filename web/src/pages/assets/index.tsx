import { useMemo, useState } from "react";
import { Link } from "react-router";
import { motion } from "motion/react";
import { ChevronDown, Search } from "lucide-react";

import { CanvasCover } from "@/components/home/canvas-cover";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { EmptyMark } from "@/components/home/empty-mark";
import { Segmented } from "@/components/home/segmented";
import { SoonTip } from "@/components/home/soon";
import { buildSampleConversations } from "@/constants/conversation-sample";
import { useCanvasList } from "@/hooks/use-canvas-list";
import { TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { formatCanvasTime } from "@/utils/home/home";
import {
  filterHistoryAssets,
  flattenHistoryAssets,
  type AssetType,
  type HistoryAsset,
} from "@/utils/home/assets";
import { placeholderBackground } from "@/utils/home/placeholder";

/** 资产范围：生成历史，或者画布 */
type Scope = "history" | "canvas";

/** 还没有接口的筛选下拉 */
const DROPDOWNS = ["筛选", "时间", "排序"];

/** 内容类型的筛选项 */
const TYPES: { value: AssetType; label: string }[] = [
  { value: "image", label: "图片" },
  { value: "video", label: "视频" },
  { value: "audio", label: "音频" },
  { value: "doc", label: "文档" },
];

/** 一张生成历史卡：悬停时底部浮出提示词和模型，视频带时长角标 */
function HistoryCard({ asset }: { asset: HistoryAsset }) {
  return (
    <motion.button
      type="button"
      whileTap={TAP}
      aria-label={asset.prompt}
      style={{ background: placeholderBackground(asset.hue) }}
      className={cn(
        "group/asset focus-visible:ring-ring/60 relative overflow-hidden rounded-xl text-left outline-none focus-visible:ring-3",
        asset.type === "video" ? "aspect-[16/10]" : "aspect-square",
      )}
    >
      {asset.duration && (
        <span className="bg-cover-scrim text-cover-foreground absolute top-2 right-2 inline-grid h-5 place-items-center rounded-md px-1.5 text-[11px] tabular-nums">
          {asset.duration}
        </span>
      )}
      <span className="from-cover-scrim text-cover-foreground absolute inset-x-0 bottom-0 bg-linear-to-t to-transparent px-2.5 pt-7 pb-2 text-xs opacity-0 transition-opacity duration-120 group-hover/asset:opacity-100 group-focus-visible/asset:opacity-100">
        <span className="line-clamp-2 leading-snug">{asset.prompt}</span>
        <span className="text-[11px] opacity-70">
          {asset.model} · {asset.day}
        </span>
      </span>
    </motion.button>
  );
}

/**
 * 资产页（设计稿 docs/品牌包装/登录与首页改版原型）：
 * 「生成历史 / 画布」两个范围；生成历史按图片、视频、音频、文档过滤，另有搜索和筛选下拉。
 * 画布范围是真的画布列表；生成历史的接口还没有，先用对话样例铺出样式，
 * 筛选、时间、排序下拉先禁用。没有内容时显示连镜 Logo 的空状态。
 */
export default function AssetsPage() {
  const [scope, setScope] = useState<Scope>("history");
  const [type, setType] = useState<AssetType>("image");
  const list = useCanvasList(60);
  const history = useMemo(() => flattenHistoryAssets(buildSampleConversations()), []);
  const visibleHistory = filterHistoryAssets(history, type, list.query);
  const inCanvas = scope === "canvas";

  const empty = inCanvas ? !list.loading && !list.error && list.items.length === 0 : visibleHistory.length === 0;
  let hint = "在「创作」里生成的内容会出现在这里";
  if (list.query.trim()) hint = "换个关键词试试";
  else if (inCanvas) hint = "新建的画布会出现在这里";

  return (
    <div className="mx-auto w-full max-w-260 px-4 pb-24 md:px-6">
      <div className="flex items-center gap-2 pt-1">
        <Segmented<Scope>
          variant="track"
          label="资产范围"
          value={scope}
          onChange={setScope}
          options={[
            { value: "history", label: "生成历史" },
            { value: "canvas", label: "画布" },
          ]}
        />
        <div className="flex-1" />
        <label className="bg-muted text-muted-foreground focus-within:ring-ring flex h-8 w-33 items-center gap-1.5 rounded-[10px] px-2.5 transition-shadow focus-within:ring-1 md:w-50">
          <Search className="size-4 shrink-0" />
          <input
            value={list.query}
            onChange={(event) => list.setQuery(event.target.value)}
            onKeyDown={(event) => event.key === "Escape" && list.setQuery("")}
            placeholder={inCanvas ? "搜索画布" : "搜索提示词"}
            aria-label="搜索资产"
            className="text-foreground min-w-0 flex-1 bg-transparent outline-none"
          />
        </label>
      </div>

      {!inCanvas && (
        <div className="mt-4 mb-4.5 flex flex-wrap items-center gap-1">
          <div className="flex gap-0.5" role="group" aria-label="内容类型">
            {TYPES.map((item) => (
              <button
                key={item.value}
                type="button"
                aria-pressed={type === item.value}
                onClick={() => setType(item.value)}
                className="text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-ring/50 aria-pressed:text-foreground h-8 rounded-lg px-3 outline-none focus-visible:ring-3 aria-pressed:font-semibold"
              >
                {item.label}
              </button>
            ))}
          </div>
          <span aria-hidden className="bg-border mx-1.5 h-4 w-px" />
          {DROPDOWNS.map((label) => (
            <SoonTip key={label} className="inline-flex cursor-not-allowed">
              <button
                type="button"
                disabled
                className="text-muted-foreground inline-flex h-8 items-center gap-1 rounded-lg px-3 disabled:opacity-60"
              >
                {label}
                <ChevronDown className="size-3.5" />
              </button>
            </SoonTip>
          ))}
        </div>
      )}
      {inCanvas && <div className="h-4" />}

      {inCanvas && list.loading ? (
        <div className="grid grid-cols-2 gap-2.5 md:grid-cols-[repeat(auto-fill,minmax(220px,1fr))]">
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="aspect-[16/10] rounded-xl" />
          ))}
        </div>
      ) : inCanvas && list.error ? (
        <EmptyMark
          title="加载失败"
          hint={
            <Button variant="outline" size="sm" onClick={list.reload}>
              重试
            </Button>
          }
        />
      ) : empty ? (
        <EmptyMark title="暂无相关资产" hint={hint} />
      ) : inCanvas ? (
        <div className="grid grid-cols-2 gap-2.5 md:grid-cols-[repeat(auto-fill,minmax(220px,1fr))]">
          {list.items.map((canvas) => (
            <motion.div key={canvas.id} whileTap={TAP} className="min-w-0">
              <Link
                to={`/canvas/${canvas.id}`}
                aria-label={`打开画布 ${canvas.title}`}
                className="text-cover-foreground focus-visible:ring-ring/60 relative block aspect-[16/10] overflow-hidden rounded-xl outline-none focus-visible:ring-3"
              >
                <CanvasCover id={canvas.id} coverUrl={canvas.coverUrl} />
                <span className="from-cover-scrim absolute inset-x-0 bottom-0 bg-linear-to-t to-transparent px-2.5 pt-7 pb-2 text-xs">
                  <span className="block truncate font-medium">{canvas.title}</span>
                  <span className="text-[11px] tabular-nums opacity-70">
                    {formatCanvasTime(canvas.updatedAt)}
                  </span>
                </span>
              </Link>
            </motion.div>
          ))}
        </div>
      ) : (
        <div className="grid grid-cols-2 gap-2.5 md:grid-cols-[repeat(auto-fill,minmax(180px,1fr))]">
          {visibleHistory.map((asset) => (
            <HistoryCard key={asset.id} asset={asset} />
          ))}
        </div>
      )}
    </div>
  );
}
