import { useCallback, useRef, useState, type ReactNode } from "react";

import { getCanvasList } from "@/api/canvas";
import type { CanvasListItemDto } from "@/api/canvas/type";
import type { FilterGroup } from "@/components/assets/filter-menu";
import { sourceToValue, type SourceFilter } from "@/utils/assets/generated-filter";

/** 来源下拉里画布列表的加载状态 */
type CanvasOptions = { status: "idle" | "loading" | "ready" | "error"; items: CanvasListItemDto[] };

/** 来源下拉里最多列出多少张画布 */
const CANVAS_OPTION_LIMIT = 60;

/**
 * 生成历史「来源」下拉的选项：全部来源、首页对话，以及用户的画布列表。
 * 画布列表第一次打开下拉时才去拉（失败后重新打开会再试）；选中的画布不在已拉到的列表里时
 * （还没打开过下拉），补一个选项让按钮文案能显示它。资产页的生成历史和画布里的资产库弹框共用。
 * @param source 当前选中的来源
 * @returns value 单选当前值；groups 选项分组；onOpenChange 与 footer 接给 FilterMenu
 */
export function useCanvasSourceMenu(source: SourceFilter): {
  value: string;
  groups: FilterGroup[];
  onOpenChange: (open: boolean) => void;
  footer: ReactNode;
} {
  const [canvases, setCanvases] = useState<CanvasOptions>({ status: "idle", items: [] });
  /** 画布列表是否已发起请求：加载中或已成功就不重复拉，失败后重新打开下拉会再试 */
  const requested = useRef(false);
  const onOpenChange = useCallback((open: boolean) => {
    if (!open || requested.current) return;
    requested.current = true;
    setCanvases((prev) => ({ status: "loading", items: prev.items }));
    getCanvasList({ page: 1, page_size: CANVAS_OPTION_LIMIT })
      .then((result) => setCanvases({ status: "ready", items: result.items }))
      .catch(() => {
        requested.current = false;
        setCanvases({ status: "error", items: [] });
      });
  }, []);

  const groups: FilterGroup[] = [
    {
      options: [
        { value: "all", label: "全部来源" },
        { value: "conversation", label: "首页对话" },
      ],
    },
    ...(canvases.items.length > 0
      ? [
          {
            heading: "画布",
            options: canvases.items.map((canvas) => ({
              value: `canvas:${canvas.id}`,
              label: canvas.title,
            })),
          },
        ]
      : []),
  ];
  const value = sourceToValue(source);
  if (source.type === "canvas" && !canvases.items.some((canvas) => canvas.id === source.canvasId)) {
    groups.push({ heading: "画布", options: [{ value, label: "指定画布" }] });
  }

  const footer =
    canvases.status === "loading" ? (
      <div className="text-muted-foreground px-2 py-1.5 text-xs">正在加载画布…</div>
    ) : canvases.status === "error" ? (
      <div className="text-muted-foreground px-2 py-1.5 text-xs">画布列表加载失败</div>
    ) : undefined;

  return { value, groups, onOpenChange, footer };
}
