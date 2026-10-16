import { useCallback, useState } from "react";
import { useSearchParams } from "react-router";

import { Segmented } from "@/components/home/segmented";
import {
  buildAssetsSearch,
  parseAssetsParams,
  type AssetsRoute,
  type AssetsTab,
} from "@/utils/assets/route-state";

import { CanvasView } from "./canvas-view";
import { HistoryView } from "./history-view";
import { LibraryView } from "./library-view";

/**
 * 资产页（设计稿 docs/design/资产库）：「生成历史 / 画布 / 个人资产库」三个范围。
 * 范围和个人资产库里的文件夹都写在地址栏（/assets?tab=library&folder=1f），刷新、后退能还原。
 * 各范围自己管内容和工具条；头部右侧留一个容器，各范围把自己的搜索框（和「新建」）用 portal 放进来，
 * 这样切换范围时顶部的分段控件不会重新挂载，滑块动画是连续的。
 */
export default function AssetsPage() {
  const [params, setParams] = useSearchParams();
  const route = parseAssetsParams(params);
  const [searchSlot, setSearchSlot] = useState<HTMLElement | null>(null);

  const go = useCallback(
    (next: AssetsRoute, options?: { replace?: boolean }) =>
      setParams(buildAssetsSearch(next), options),
    [setParams],
  );
  const changeFolder = useCallback(
    (folder: string | null, options?: { replace?: boolean }) =>
      go({ tab: "library", folder }, options),
    [go],
  );

  return (
    <div className="mx-auto w-full max-w-260 px-4 pb-24 md:px-6">
      <div className="flex flex-wrap items-center gap-2 pt-1">
        <Segmented<AssetsTab>
          variant="track"
          label="资产范围"
          value={route.tab}
          onChange={(tab) => go({ tab, folder: null })}
          options={[
            { value: "history", label: "生成历史" },
            { value: "canvas", label: "画布" },
            { value: "library", label: "个人资产库" },
          ]}
        />
        <div ref={setSearchSlot} className="ml-auto flex items-center gap-2" />
      </div>

      {route.tab === "history" && <HistoryView searchSlot={searchSlot} />}
      {route.tab === "canvas" && <CanvasView searchSlot={searchSlot} />}
      {route.tab === "library" && (
        <LibraryView searchSlot={searchSlot} folder={route.folder} onFolderChange={changeFolder} />
      )}
    </div>
  );
}
