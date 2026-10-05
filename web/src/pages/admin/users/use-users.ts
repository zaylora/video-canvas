import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";

import { getUser, listUsers } from "@/api/admin/users";
import type { UserDetail, UserListItem, UserRole, UserStatus } from "@/api/admin/users/type.d";
import { isUserNotFound } from "@/utils/admin/user-rules";

/** 列表默认每页条数 */
export const DEFAULT_PAGE_SIZE = 20;

/** 抽屉页签；同步到 ?tab= */
export type UserTab = "overview" | "tasks" | "credits" | "logins";

/** 状态筛选：all 为不筛 */
export type StatusFilter = "all" | UserStatus;

/** 角色筛选：all 为不筛 */
export type RoleFilter = "all" | UserRole;

const TABS: UserTab[] = ["overview", "tasks", "credits", "logins"];
const STATUSES: StatusFilter[] = ["all", "active", "disabled"];
const ROLES: RoleFilter[] = ["all", "user", "admin", "super_admin"];

const pick = <T extends string>(value: string | null, allowed: T[], fallback: T): T =>
  allowed.includes(value as T) ? (value as T) : fallback;

/** 列表加载状态：loading 是还没有任何数据，刷新时保留旧数据不回到 loading */
export type ListState = "loading" | "ready" | "error";

/**
 * 用户列表：q / status / role / page / user / tab 都在 URL 里（刷新不丢，也能把链接发给同事）。
 * 翻页和筛选时保留旧数据（refreshing 为 true），首次加载没有数据才是 loading。
 * 所有 URL 更新都用 replace：↑↓ 连续切换用户不该在历史记录里堆一串。
 */
export function useUsers() {
  const [params, setParams] = useSearchParams();
  const q = params.get("q") ?? "";
  const status = pick(params.get("status"), STATUSES, "all");
  const role = pick(params.get("role"), ROLES, "all");
  const page = Math.max(1, Math.floor(Number(params.get("page"))) || 1);
  const userParam = Math.floor(Number(params.get("user")));
  const userId = userParam > 0 ? userParam : null;
  const tab = pick(params.get("tab"), TABS, "overview");

  const [pageSize, setPageSizeState] = useState(DEFAULT_PAGE_SIZE);
  const [items, setItems] = useState<UserListItem[]>([]);
  const [total, setTotal] = useState(0);
  const [overallTotal, setOverallTotal] = useState<number | null>(null);
  const [state, setState] = useState<ListState>("loading");
  const [loadedPage, setLoadedPage] = useState<number | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [version, setVersion] = useState(0);
  const seq = useRef(0);
  /** 抽屉里 ↑↓ 走出当前页后翻页：记下目标页与方向，列表回来时自动打开新页的首条（下一个）或末条（上一个） */
  const pendingStep = useRef<{ dir: 1 | -1; page: number } | null>(null);

  /**
   * 改 URL 参数。react-router 的 setSearchParams 每次 URL 变化都换身份，
   * 这里用 ref 兜一层，让 update 以及下面所有操作函数保持稳定，不会反复触发依赖它们的 effect。
   */
  const setParamsRef = useRef(setParams);
  useEffect(() => {
    setParamsRef.current = setParams;
  });
  const update = useCallback((patch: Record<string, string | null>) => {
    setParamsRef.current(
      (prev) => {
        const next = new URLSearchParams(prev);
        for (const [key, value] of Object.entries(patch)) {
          if (value === null || value === "") next.delete(key);
          else next.set(key, value);
        }
        return next;
      },
      { replace: true },
    );
  }, []);

  const filtering = q.trim() !== "" || status !== "all" || role !== "all";

  useEffect(() => {
    const mine = ++seq.current;
    setRefreshing(true);
    listUsers({
      q: q.trim() || undefined,
      status: status === "all" ? undefined : status,
      role: role === "all" ? undefined : role,
      page,
      page_size: pageSize,
    })
      .then((result) => {
        if (mine !== seq.current) return;
        setItems(result.list);
        setTotal(result.total);
        const pending = pendingStep.current;
        if (pending && pending.page === page) {
          pendingStep.current = null;
          const target = pending.dir > 0 ? result.list[0] : result.list[result.list.length - 1];
          if (target) update({ user: String(target.id) });
        }
        setState("ready");
        setLoadedPage(page);
        if (!filtering) setOverallTotal(result.total);
      })
      .catch(() => {
        // 全局 toast 已由拦截器弹；这里只切到可重试的失败态
        if (mine === seq.current) setState("error");
      })
      .finally(() => {
        if (mine === seq.current) setRefreshing(false);
      });
  }, [q, status, role, page, pageSize, version, filtering, update]);

  /** 有筛选时「筛出 N / 总数 人」需要总数：没见过无筛选的总数就单独问一次 */
  useEffect(() => {
    if (!filtering || overallTotal !== null) return;
    let alive = true;
    listUsers({ page: 1, page_size: 1 })
      .then((result) => alive && setOverallTotal(result.total))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, [filtering, overallTotal]);

  /** 重新拉取当前页；不清空旧数据 */
  const reload = useCallback(() => setVersion((v) => v + 1), [setVersion]);

  /** 本地改一行（接口已返回新值时用，让数字原地滚动，不等重新拉列表） */
  const patchItem = useCallback(
    (id: number, patch: Partial<UserListItem>) => {
      setItems((list) => list.map((item) => (item.id === id ? { ...item, ...patch } : item)));
    },
    [setItems],
  );

  const actions = useMemo(
    () => ({
      /** 搜索词；改变后回到第 1 页 */
      setQ: (value: string) => update({ q: value, page: null }),
      setStatus: (value: StatusFilter) =>
        update({ status: value === "all" ? null : value, page: null }),
      setRole: (value: RoleFilter) => update({ role: value === "all" ? null : value, page: null }),
      setPage: (value: number) => update({ page: value > 1 ? String(value) : null }),
      setPageSize: (value: number) => {
        setPageSizeState(value);
        update({ page: null });
      },
      /** 清除全部筛选（保留抽屉） */
      clearFilters: () => update({ q: null, status: null, role: null, page: null }),
      /** 打开某个用户的抽屉；换用户时保留页签 */
      openUser: (id: number) => update({ user: String(id) }),
      /** 关闭抽屉，同时去掉 user 和 tab */
      closeUser: () => update({ user: null, tab: null }),
      setTab: (value: UserTab) => update({ tab: value === "overview" ? null : value }),
      /** 走出当前页：翻到目标页，数据回来后自动打开首条（dir=1）或末条（dir=-1） */
      stepToPage: (dir: 1 | -1, target: number) => {
        pendingStep.current = { dir, page: target };
        update({ page: target > 1 ? String(target) : null });
      },
    }),
    [update, setPageSizeState],
  );

  return {
    q,
    status,
    role,
    page,
    pageSize,
    userId,
    tab,
    items,
    total,
    overallTotal,
    filtering,
    state,
    /** 最近一次成功加载的页码：翻页后据此判断新一页的数据是否已经回来 */
    loadedPage,
    refreshing,
    reload,
    patchItem,
    ...actions,
  };
}

/** 抽屉里的详情状态：notfound 对应 URL 里的用户已不存在 */
export type DetailState = "loading" | "ready" | "error" | "notfound";

/**
 * 抽屉里的用户详情。切换用户时先清掉旧详情再拉新的，保证头部和摘要不会短暂显示上一个人的数据。
 * @param id 当前打开的用户；null 表示抽屉关着，不请求
 */
export function useUserDetail(id: number | null) {
  const [detail, setDetail] = useState<UserDetail | null>(null);
  const [state, setState] = useState<DetailState>("loading");
  const [version, setVersion] = useState(0);
  const seq = useRef(0);
  const loadedId = useRef<number | null>(null);

  useEffect(() => {
    if (id === null) return;
    const mine = ++seq.current;
    if (loadedId.current !== id) {
      setDetail(null);
      setState("loading");
    }
    getUser(id)
      .then((result) => {
        if (mine !== seq.current) return;
        loadedId.current = id;
        setDetail(result);
        setState("ready");
      })
      .catch((error: unknown) => {
        if (mine !== seq.current) return;
        // 已经有这个用户的详情时，刷新失败保留旧数据；全局 toast 已弹
        if (loadedId.current === id) return;
        setState(isUserNotFound(error) ? "notfound" : "error");
      });
  }, [id, version]);

  const reload = useCallback(() => setVersion((v) => v + 1), []);
  /** 只改 id 对得上的那份详情：行上的操作可能针对的不是抽屉里这个人 */
  const patch = useCallback((targetId: number, patchValue: Partial<UserDetail>) => {
    setDetail((value) => (value && value.id === targetId ? { ...value, ...patchValue } : value));
  }, []);

  /** 切换用户的瞬间 detail 还是上一个人的：id 对不上就当没有 */
  const current = detail && detail.id === id ? detail : null;
  return {
    detail: current,
    state: current ? state : state === "ready" ? "loading" : state,
    reload,
    patch,
  };
}
