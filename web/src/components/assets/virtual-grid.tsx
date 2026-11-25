import {
  observeWindowOffset,
  useVirtualizer,
  useWindowVirtualizer,
  type VirtualItem,
} from "@tanstack/react-virtual";
import { useLayoutEffect, useRef, useState, type ReactNode } from "react";

import { cn } from "@/lib/utils";
import { gridColumns, type GridColumnSpec } from "@/utils/assets/grid-columns";

/** 首屏（含服务端渲染 / 测试）还没量过滚动区时按这个尺寸出行，挂载后由真实尺寸接管 */
const INITIAL_RECT = { width: 1024, height: 800 };

const SCROLL_LOCK_ATTRIBUTE = "data-base-ui-scroll-locked";

/** Base UI 锁定带常驻滚动条的页面时，可能把文档的滚动偏移转移到 body。 */
function documentScrollOffset(win: Window) {
  const { documentElement, body } = win.document;
  return win.scrollY + (documentElement.hasAttribute(SCROLL_LOCK_ATTRIBUTE) ? body.scrollTop : 0);
}

/** 锁定状态变化不一定伴随 window 的 scroll 事件。 */
const observeDocumentOffset: typeof observeWindowOffset = (instance, onOffset) => {
  const win = instance.scrollElement;
  if (!win) return;

  const update = () => onOffset(documentScrollOffset(win), false);
  const { documentElement, body } = win.document;
  const observer = new MutationObserver(update);
  observer.observe(documentElement, { attributes: true, attributeFilter: [SCROLL_LOCK_ATTRIBUTE] });
  body.addEventListener("scroll", update, { passive: true });
  const unobserveWindow = observeWindowOffset(instance, (_offset, isScrolling) => {
    onOffset(documentScrollOffset(win), isScrolling);
  });

  return () => {
    observer.disconnect();
    body.removeEventListener("scroll", update);
    unobserveWindow?.();
  };
};

/** 两种虚拟器（整页 / 元素）共有、渲染行时用到的那部分 */
interface RowVirtualizer {
  getVirtualItems: () => VirtualItem[];
  getTotalSize: () => number;
  measureElement: (node: Element | null) => void;
  options: { scrollMargin: number };
}

/** 往上找最近的可纵向滚动的祖先；找不到说明滚动的是整页 */
function scrollParentOf(el: HTMLElement): HTMLElement | null {
  for (let node = el.parentElement; node; node = node.parentElement) {
    const overflowY = getComputedStyle(node).overflowY;
    if (overflowY === "auto" || overflowY === "scroll") return node;
  }
  return null;
}

/**
 * 量网格容器：宽度（算列数）、顶边在滚动内容里的位置（scrollMargin），以及父级滚动容器。
 * @param mode window 跟随整页滚动；parent 跟随最近的可滚动祖先（如弹框里的内容区）
 */
function useGridLayout(mode: "window" | "parent") {
  const root = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  const [margin, setMargin] = useState(0);
  const [scroller, setScroller] = useState<HTMLElement | null>(null);

  useLayoutEffect(() => {
    const el = root.current;
    if (!el) return;
    const parent = mode === "parent" ? scrollParentOf(el) : null;
    const measure = () => {
      setScroller(parent);
      setWidth(el.clientWidth);
      const top = el.getBoundingClientRect().top;
      // 滚动区内的位置 = 相对滚动区可视顶边的距离 + 已滚出去的距离
      setMargin(
        parent
          ? top - parent.getBoundingClientRect().top + parent.scrollTop
          : top + documentScrollOffset(window),
      );
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [mode]);

  return { root, width, margin, scroller };
}

/** 一行一个 grid，行内的列数与间距由调用方给；行距放在行内（下内边距），量到的行高就已含行距 */
function GridRows<T>({
  virtual,
  root,
  items,
  cols,
  columns,
  getKey,
  renderItem,
  className,
  ...rest
}: {
  virtual: RowVirtualizer;
  root: React.RefObject<HTMLDivElement | null>;
  items: readonly T[];
  cols: number;
  columns: GridColumnSpec;
  getKey: (item: T) => string;
  renderItem: (item: T) => ReactNode;
  className?: string;
  "data-slot"?: string;
}) {
  return (
    <div
      ref={root}
      className={cn("relative w-full", className)}
      style={{ height: virtual.getTotalSize() }}
      {...rest}
    >
      {virtual.getVirtualItems().map((row) => (
        <div
          key={row.index}
          ref={virtual.measureElement}
          data-index={row.index}
          className="absolute top-0 left-0 grid w-full"
          style={{
            gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))`,
            columnGap: columns.gap,
            paddingBottom: columns.gap,
            transform: `translateY(${row.start - virtual.options.scrollMargin}px)`,
          }}
        >
          {items.slice(row.index * cols, row.index * cols + cols).map((item) => (
            <div key={getKey(item)} className="min-w-0">
              {renderItem(item)}
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}

interface VirtualGridProps<T> {
  /** 全部条目 */
  items: readonly T[];
  /** 条目的稳定 key */
  getKey: (item: T) => string;
  /** 渲染一个条目；条目要自己撑满单元格宽度（w-full），外层不会替它拉伸 */
  renderItem: (item: T) => ReactNode;
  /** 列规格 */
  columns: GridColumnSpec;
  /** 按单元格宽度估一行的高度（含行距），挂载后会被量到的真实高度取代 */
  estimateRowHeight: (cellWidth: number) => number;
  /** 滚动的是整页（默认）还是最近的可滚动祖先（弹框里的内容区） */
  scroll?: "window" | "parent";
  className?: string;
  "data-slot"?: string;
}

/**
 * 虚拟网格：只挂载视口附近的几行，其余用占位撑高。
 * 一行放几张按容器宽度算（和 CSS 的 auto-fill 同一个规则），每行的实际高度由渲染出来的行量取。
 * 整页滚动与弹框内滚动是两种虚拟器，hook 不能按条件调用，所以拆成两个组件，行的渲染共用 GridRows。
 */
export function VirtualGrid<T>(props: VirtualGridProps<T>) {
  return props.scroll === "parent" ? (
    <ParentScrollGrid {...props} />
  ) : (
    <WindowScrollGrid {...props} />
  );
}

function WindowScrollGrid<T>({
  items,
  columns,
  estimateRowHeight,
  scroll: _scroll,
  ...rest
}: VirtualGridProps<T>) {
  const { root, width, margin } = useGridLayout("window");
  const cols = gridColumns(width, columns);
  const cellWidth = width > 0 ? (width - columns.gap * (cols - 1)) / cols : columns.minCell;

  // useWindowVirtualizer 的返回值不能被 React Compiler 记忆化；只在本组件内使用
  // oxlint-disable-next-line react/incompatible-library
  const virtual = useWindowVirtualizer({
    observeElementOffset: observeDocumentOffset,
    initialOffset: () => (typeof window === "undefined" ? 0 : documentScrollOffset(window)),
    count: Math.ceil(items.length / cols),
    estimateSize: () => estimateRowHeight(cellWidth),
    overscan: 3,
    scrollMargin: margin,
    initialRect: INITIAL_RECT,
  });

  return (
    <GridRows virtual={virtual} root={root} items={items} cols={cols} columns={columns} {...rest} />
  );
}

function ParentScrollGrid<T>({
  items,
  columns,
  estimateRowHeight,
  scroll: _scroll,
  ...rest
}: VirtualGridProps<T>) {
  const { root, width, margin, scroller } = useGridLayout("parent");
  const cols = gridColumns(width, columns);
  const cellWidth = width > 0 ? (width - columns.gap * (cols - 1)) / cols : columns.minCell;

  // oxlint-disable-next-line react/incompatible-library
  const virtual = useVirtualizer({
    count: Math.ceil(items.length / cols),
    getScrollElement: () => scroller,
    estimateSize: () => estimateRowHeight(cellWidth),
    overscan: 3,
    scrollMargin: margin,
    initialRect: INITIAL_RECT,
  });

  return (
    <GridRows virtual={virtual} root={root} items={items} cols={cols} columns={columns} {...rest} />
  );
}
