import { MotionConfig, motion } from "motion/react";
import { useCallback, useState } from "react";
import { useSearchParams } from "react-router";

import { Segmented } from "@/components/home/segmented";
import {
  buildAssetsSearch,
  parseAssetsParams,
  type AssetsRoute,
  type AssetsTab,
} from "@/utils/assets/route-state";

import { HistoryView } from "./history-view";
import { LibraryView } from "./library-view";

/** 两个范围从左到右的顺序，决定切换时内容滑入的方向 */
const TAB_ORDER: readonly AssetsTab[] = ["history", "library"];

/**
 * 资产页（设计稿 docs/design/资产库）：大标题旁是「生成历史 / 个人资产库」两个范围。
 * 范围和个人资产库里的文件夹都写在地址栏（/assets?tab=library&folder=1f），刷新、后退能还原。
 * 各范围自己管内容和下面那条半透明工具条（筛选、搜索、新建）；分段控件放在标题旁不随范围重新挂载，滑块动画是连续的。
 */
export default function AssetsPage() {
  const [params, setParams] = useSearchParams();
  const route = parseAssetsParams(params);
  // 范围变了就记下切换方向：往右边的范围切，内容从右往左滑入，反之亦然（文件夹切换不算）
  const [prev, setPrev] = useState({ tab: route.tab, from: 16 });
  if (prev.tab !== route.tab) {
    const forward = TAB_ORDER.indexOf(route.tab) > TAB_ORDER.indexOf(prev.tab);
    setPrev({ tab: route.tab, from: forward ? 16 : -16 });
  }

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
    <div className="mx-auto w-full max-w-320 px-4 pb-24 md:px-10">
      <div className="flex flex-wrap items-center gap-5 pt-6 md:pt-10">
        <h1 className="text-[28px] font-semibold tracking-[-0.025em] md:text-[34px]">资产</h1>
        <Segmented<AssetsTab>
          variant="track"
          label="资产范围"
          value={route.tab}
          onChange={(tab) => go({ tab, folder: null })}
          options={[
            { value: "history", label: "生成历史" },
            { value: "library", label: "个人资产库" },
          ]}
        />
      </div>

      <MotionConfig reducedMotion="user">
        <motion.div
          key={route.tab}
          initial={{ opacity: 0, x: prev.from }}
          animate={{ opacity: 1, x: 0 }}
          transition={{ duration: 0.28, ease: [0.22, 1, 0.36, 1] }}
        >
          {route.tab === "history" && <HistoryView />}
          {route.tab === "library" && (
            <LibraryView folder={route.folder} onFolderChange={changeFolder} />
          )}
        </motion.div>
      </MotionConfig>
    </div>
  );
}
