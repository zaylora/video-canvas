import { useState } from "react";
import { motion } from "motion/react";
import { Box, GripVertical, Image as ImageIcon, User } from "lucide-react";

import { StatusPill } from "@/components/drama/status-pill";
import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import {
  groupByType,
  splitByEpisode,
  type DramaAssetType,
  type RefAsset,
} from "@/utils/drama/segment-assets";

import { useStudio } from "./use-studio";

/** 拖拽时放在 dataTransfer 里的类型：提示词编辑器据此认出这是一个资产 */
export const ASSET_DRAG_TYPE = "application/x-drama-asset";

const TYPE_ICON: Record<DramaAssetType, typeof User> = {
  character: User,
  scene: ImageIcon,
  prop: Box,
};

/** 资产的缩略图地址：就是节点上的素材（角色卡节点的主图也在 src 里） */
export function useAssetThumb(asset: RefAsset): string | null {
  return asset.src;
}

/** 有没有能当参考图的形象：角色卡节点看主图，其余看节点素材；正在生成的不算（assetId 还指着上一版） */
export function assetHasImage(asset: RefAsset): boolean {
  return !!asset.src && !asset.generating;
}

function Item({ asset }: { asset: RefAsset }) {
  const { insertRef } = useStudio();
  const thumb = useAssetThumb(asset);
  const Icon = TYPE_ICON[asset.type];
  const noImage = !assetHasImage(asset);
  return (
    <button
      type="button"
      draggable
      title="拖到提示词里，或点击插入到光标处"
      onClick={() => insertRef(asset)}
      onDragStart={(event) => {
        event.dataTransfer.setData(
          ASSET_DRAG_TYPE,
          JSON.stringify({ id: asset.id, label: asset.label }),
        );
        // 拖到别处（如搜索框）时落成资产名；提示词编辑器优先认上面那个类型
        event.dataTransfer.setData("text/plain", asset.label);
        event.dataTransfer.effectAllowed = "copy";
      }}
      className="hover:bg-chrome-hover focus-visible:ring-ring/50 mx-1 flex w-[calc(100%-0.5rem)] cursor-grab items-center gap-2 rounded-lg px-2 py-1.5 text-left text-xs outline-none focus-visible:ring-2 active:cursor-grabbing"
    >
      <span className="bg-muted text-muted-foreground grid size-[30px] shrink-0 place-items-center overflow-hidden rounded-[7px]">
        {thumb ? (
          <img src={thumb} alt="" draggable={false} className="size-full object-cover" />
        ) : (
          <Icon className="size-3.5" />
        )}
      </span>
      <span className="min-w-0 flex-1 truncate">{asset.label}</span>
      {noImage && (
        <StatusPill tone="warning" className="h-4 text-[10px]">
          无图
        </StatusPill>
      )}
      <GripVertical className="text-muted-foreground size-3.5 shrink-0" />
    </button>
  );
}

/** 本集 / 全集切换：滑块用 layoutId 在两项之间滑动 */
function Scope({ all, onChange }: { all: boolean; onChange: (all: boolean) => void }) {
  return (
    <div role="group" aria-label="资产范围" className="bg-muted relative flex rounded-lg p-0.5">
      {(
        [
          [false, "本集"],
          [true, "全集"],
        ] as const
      ).map(([value, label]) => (
        <button
          key={label}
          type="button"
          aria-pressed={all === value}
          onClick={() => onChange(value)}
          className={cn(
            "focus-visible:ring-ring/50 relative h-6 rounded-md px-2.5 text-xs font-medium outline-none focus-visible:ring-2",
            all === value ? "text-foreground" : "text-muted-foreground",
          )}
        >
          {all === value && (
            <motion.span
              layoutId="studio-lib-scope"
              transition={SPRING}
              className="bg-background absolute inset-0 rounded-md shadow-sm"
            />
          )}
          <span className="relative">{label}</span>
        </button>
      ))}
    </div>
  );
}

/** 左资产库：本集 / 全集，按角色 / 场景 / 道具分组，无图资产标出；可拖进提示词 */
export function Library() {
  const { assets, assetsStatus, no } = useStudio();
  const [all, setAll] = useState(false);
  const shown = all ? assets : splitByEpisode(assets, no).here;
  const groups = groupByType(shown);
  return (
    <aside
      aria-label="资产库"
      className="border-border flex w-56 shrink-0 flex-col border-r max-xl:hidden"
    >
      <div className="flex items-center px-3 pt-3 pb-2">
        <b className="text-sm">资产库</b>
        <span className="flex-1" />
        <Scope all={all} onChange={setAll} />
      </div>
      <p className="text-muted-foreground px-3 pb-1.5 text-[11px]">
        拖到提示词里，或在提示词里输入 <kbd className="bg-muted rounded px-1">@</kbd>
      </p>
      <div className="min-h-0 flex-1 overflow-y-auto pb-3">
        {groups.map((group) => {
          const Icon = TYPE_ICON[group.type];
          return (
            <div key={group.type}>
              <div className="text-muted-foreground flex items-center gap-1.5 px-3 pt-2 pb-1 text-[11px] [&_svg]:size-3.5">
                <Icon />
                {group.label}
                <span className="tabular-nums">{group.items.length}</span>
              </div>
              {group.items.map((asset) => (
                <Item key={asset.id} asset={asset} />
              ))}
            </div>
          );
        })}
        {groups.length === 0 && (
          <p className="text-muted-foreground px-3 py-8 text-center text-xs">
            {assetsStatus === "loading"
              ? "加载中…"
              : assetsStatus === "error"
                ? "资产读取失败，切到别的标签再回来会重试"
                : all
                  ? "还没有资产，先在资产页做拆解"
                  : "这一集还没有资产，可以切到「全集」看看"}
          </p>
        )}
      </div>
    </aside>
  );
}
