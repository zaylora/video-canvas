/** 一页数据；nextCursor 为空串表示没有更多 */
export type PagedPage<T> = { items: T[]; nextCursor: string };

/** 游标分页列表的状态 */
export type PagedState<T> = {
  /** 已加载的全部内容 */
  items: T[];
  /** 下一页游标；空串表示到底了 */
  nextCursor: string;
  /** loading 首页加载中；ready 有内容（可能为空）；error 首页失败 */
  status: "loading" | "ready" | "error";
  /** 正在加载下一页 */
  loadingMore: boolean;
  /** 下一页加载失败过：滚动到底不再自动重试，要用户点「重试」 */
  moreFailed: boolean;
};

/** 游标分页列表：不依赖 React，hooks 用 useSyncExternalStore 订阅 */
export type PagedList<T> = {
  /** 当前状态；内容没变时引用不变 */
  getState: () => PagedState<T>;
  /** 订阅变化，返回取消函数 */
  subscribe: (listener: () => void) => () => void;
  /** 从头加载首页；正在进行的请求作废 */
  load: () => void;
  /** 加载下一页；没有更多、正在加载或首页没回来时什么都不做 */
  loadMore: () => void;
  /** 首页失败后重试 */
  retry: () => void;
  /** 就地改列表内容（乐观更新、本地插入），其余状态不变 */
  update: (updater: (items: T[]) => T[]) => void;
  /** 作废所有进行中的请求（卸载时调用）；之后仍可再 load */
  cancel: () => void;
};

/**
 * 创建游标分页列表。每次 load 都开启新的一代，旧请求晚到的结果按代号丢弃，
 * 所以切换筛选条件时不会被慢请求覆盖。失败的错误提示由请求层统一弹，这里只记状态。
 * @param fetchPage 取一页：不传游标就是首页
 * @returns 分页列表
 */
export function createPagedList<T>(
  fetchPage: (cursor: string | undefined) => Promise<PagedPage<T>>,
): PagedList<T> {
  let state: PagedState<T> = {
    items: [],
    nextCursor: "",
    status: "loading",
    loadingMore: false,
    moreFailed: false,
  };
  let generation = 0;
  const listeners = new Set<() => void>();

  const set = (patch: Partial<PagedState<T>>) => {
    state = { ...state, ...patch };
    listeners.forEach((listener) => listener());
  };

  const load = () => {
    const mine = ++generation;
    set({ items: [], nextCursor: "", status: "loading", loadingMore: false, moreFailed: false });
    fetchPage(undefined).then(
      (page) => {
        if (mine === generation)
          set({ items: page.items, nextCursor: page.nextCursor, status: "ready" });
      },
      () => {
        if (mine === generation) set({ status: "error" });
      },
    );
  };

  const loadMore = () => {
    if (state.status !== "ready" || state.loadingMore || state.nextCursor === "") return;
    const mine = generation;
    set({ loadingMore: true, moreFailed: false });
    fetchPage(state.nextCursor).then(
      (page) => {
        if (mine === generation) {
          set({
            items: [...state.items, ...page.items],
            nextCursor: page.nextCursor,
            loadingMore: false,
          });
        }
      },
      () => {
        if (mine === generation) set({ loadingMore: false, moreFailed: true });
      },
    );
  };

  return {
    getState: () => state,
    subscribe: (listener) => {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    load,
    loadMore,
    retry: () => {
      if (state.status === "error") load();
    },
    update: (updater) => set({ items: updater(state.items) }),
    cancel: () => {
      generation++;
    },
  };
}
