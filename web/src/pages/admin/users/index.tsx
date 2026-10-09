import { Loader2, X } from "lucide-react";
import { MotionConfig } from "motion/react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { DataTablePagination } from "@/components/admin-ui/data-table-pagination";
import { NativeSelect } from "@/components/admin-ui/native-select";
import { PageHeader, PageHeaderHeading, PageHeaderTitle } from "@/components/admin-ui/page-header";
import { SearchInput } from "@/components/admin-ui/search-input";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useAdminStore } from "@/store/admin";
import type { Actor } from "@/utils/admin/user-rules";

import { BulkBar } from "./bulk-bar";
import { useUserActions } from "./use-user-actions";
import { useUserDetail, useUsers, type RoleFilter, type StatusFilter } from "./use-users";
import { UserDialog } from "./user-dialog";
import { UserTable } from "./user-table";

const STATUS_OPTIONS: { value: StatusFilter; label: string }[] = [
  { value: "all", label: "全部" },
  { value: "active", label: "正常" },
  { value: "disabled", label: "已停用" },
];

const ROLE_OPTIONS: { value: RoleFilter; label: string }[] = [
  { value: "all", label: "全部角色" },
  { value: "user", label: "普通用户" },
  { value: "admin", label: "管理员" },
  { value: "super_admin", label: "超级管理员" },
];

/** 搜索框停止输入多久后才去查询（毫秒） */
const SEARCH_DEBOUNCE = 300;

/** 这些浮层打开时，单键快捷键和 Esc 都交给浮层自己（Base UI 会关掉它） */
const FLOATING_OPEN = [
  "popover-content",
  "dropdown-menu-content",
  "dropdown-menu-sub-content",
  "dialog-content",
  "alert-dialog-content",
]
  .map((slot) => `[data-slot="${slot}"][data-open]`)
  .join(",");

/** 焦点在文本输入里：单键快捷键不该响应；勾选框、按钮不算 */
const isTyping = (target: EventTarget | null) => {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable || target.tagName === "TEXTAREA" || target.tagName === "SELECT") {
    return true;
  }
  if (target.tagName !== "INPUT") return false;
  return !["checkbox", "radio", "button", "submit"].includes((target as HTMLInputElement).type);
};

/**
 * 用户管理页：表格 + 详情弹窗（带遮罩，点遮罩关闭）+ 勾选后的批量条。
 * q / status / role / page / user / tab 都在 URL 里。
 * 快捷键：/ 聚焦搜索；弹窗打开时 ↑ ↓ / K J 切换用户；Esc 按层级依次关闭（浮层 → 勾选 → 弹窗）。
 * 请求失败的全局 toast 由拦截器弹，这里不重复。
 */
export default function UsersPage() {
  const list = useUsers();
  const role = useAdminStore((state) => state.role);
  const myId = useAdminStore((state) => state.userId);
  const actor = useMemo<Actor | null>(() => (role ? { id: myId ?? -1, role } : null), [role, myId]);

  const { userId, items, page } = list;
  const detail = useUserDetail(userId);
  /** 对某个用户做完写操作加一，弹窗页签里的列表据此重新拉取 */
  const [mutated, setMutated] = useState(0);

  const { patchItem, reload } = list;
  const { patch: patchDetail, reload: reloadDetail } = detail;
  const patch = useCallback(
    (id: number, value: Parameters<typeof patchItem>[1]) => {
      patchItem(id, value);
      patchDetail(id, value);
    },
    [patchItem, patchDetail],
  );
  const refresh = useCallback(
    (id?: number) => {
      reload();
      if (id === undefined || id === userId) reloadDetail();
      setMutated((n) => n + 1);
    },
    [reload, reloadDetail, userId],
  );
  const actions = useUserActions({ actor, patch, refresh });

  // ---------------------------------------------------------------- 搜索（300ms 防抖，同步到 ?q=）
  const [qInput, setQInput] = useState(list.q);
  const searchRef = useRef<HTMLInputElement>(null);
  const { setQ } = list;
  useEffect(() => setQInput(list.q), [list.q]);
  useEffect(() => {
    if (qInput === list.q) return;
    const timer = setTimeout(() => setQ(qInput), SEARCH_DEBOUNCE);
    return () => clearTimeout(timer);
  }, [qInput, list.q, setQ]);

  // ---------------------------------------------------------------- 勾选（只在当前页，换页 / 筛选后清空）
  const [selected, setSelected] = useState<Set<number>>(() => new Set());
  const lastChecked = useRef<number | null>(null);
  const [bulkCreditOpen, setBulkCreditOpen] = useState(false);
  useEffect(() => {
    setSelected(new Set());
    lastChecked.current = null;
  }, [list.q, list.status, list.role, page, list.pageSize]);

  const pageIds = useMemo(() => items.map((item) => item.id), [items]);
  const selectedUsers = useMemo(
    () => items.filter((item) => selected.has(item.id)),
    [items, selected],
  );
  const selection = useMemo(
    () => ({
      selected: new Set(selectedUsers.map((item) => item.id)),
      onToggle: (id: number, checked: boolean, shift: boolean) =>
        setSelected((prev) => {
          const next = new Set(prev);
          const from = lastChecked.current === null ? -1 : pageIds.indexOf(lastChecked.current);
          const to = pageIds.indexOf(id);
          // Shift + 点击：连选上一次点到这一次之间的所有行
          const range =
            shift && from >= 0 ? pageIds.slice(Math.min(from, to), Math.max(from, to) + 1) : [id];
          for (const key of range) {
            if (checked) next.add(key);
            else next.delete(key);
          }
          lastChecked.current = id;
          return next;
        }),
      onTogglePage: (checked: boolean) => setSelected(checked ? new Set(pageIds) : new Set()),
    }),
    [pageIds, selectedUsers],
  );
  const clearSelection = useCallback(() => {
    setSelected(new Set());
    lastChecked.current = null;
  }, []);

  // ---------------------------------------------------------------- 弹窗：打开 / 关闭 / 上一个下一个
  const index = userId === null ? -1 : pageIds.indexOf(userId);
  const totalPages = Math.max(1, Math.ceil(list.total / list.pageSize));
  const canPrev = index > 0 || (index === 0 && page > 1);
  const canNext =
    (index >= 0 && index < pageIds.length - 1) ||
    (index === pageIds.length - 1 && index >= 0 && page < totalPages);

  const { openUser, closeUser, setPage, stepToPage, loadedPage } = list;

  /** 切换用户（↑↓、翻页后落点、URL 直达）后，把当前行滚进视野 */
  useEffect(() => {
    if (userId === null) return;
    const frame = requestAnimationFrame(() =>
      document
        .querySelector<HTMLElement>(`[data-user-id="${userId}"]`)
        ?.scrollIntoView({ block: "nearest" }),
    );
    return () => cancelAnimationFrame(frame);
  }, [userId, loadedPage]);
  const step = useCallback(
    (dir: 1 | -1) => {
      if (index < 0) return;
      const next = pageIds[index + dir];
      if (next !== undefined) {
        openUser(next);
        return;
      }
      // 走出当前页：翻页，等新一页回来后落在首条（下一个）或末条（上一个）
      const target = page + dir;
      if (target < 1 || target > totalPages) return;
      stepToPage(dir, target);
    },
    [index, pageIds, openUser, page, totalPages, stepToPage],
  );

  const closeDialog = useCallback(() => {
    const id = userId;
    closeUser();
    // 关闭后焦点回到原来那一行
    requestAnimationFrame(() =>
      document.querySelector<HTMLElement>(`[data-user-id="${id}"]`)?.focus({ preventScroll: true }),
    );
  }, [userId, closeUser]);

  // ---------------------------------------------------------------- 快捷键
  /** 用 ref 拿最新值：监听器只绑一次，不用每次渲染重绑 */
  const keys = useRef({ step, closeDialog, clearSelection, selectedCount: 0, userId });
  useEffect(() => {
    keys.current = {
      step,
      closeDialog,
      clearSelection,
      selectedCount: selectedUsers.length,
      userId,
    };
  });
  useEffect(() => {
    /** 捕获阶段先看一眼：浮层还开着就让它自己处理 Esc，免得同一下按键又清了勾选 */
    const onKeyDown = (event: KeyboardEvent) => {
      const k = keys.current;
      if (event.key === "Escape") {
        if (document.querySelector(FLOATING_OPEN)) return;
        if (isTyping(event.target)) {
          (event.target as HTMLElement).blur();
          return;
        }
        if (k.selectedCount > 0) k.clearSelection();
        else if (k.userId !== null) k.closeDialog();
        return;
      }
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      if (isTyping(event.target) || document.querySelector(FLOATING_OPEN)) return;
      if (event.key === "/") {
        event.preventDefault();
        searchRef.current?.focus();
        searchRef.current?.select();
        return;
      }
      if (k.userId === null) return;
      if (event.key === "ArrowDown" || event.key === "j") {
        event.preventDefault();
        k.step(1);
      } else if (event.key === "ArrowUp" || event.key === "k") {
        event.preventDefault();
        k.step(-1);
      }
    };
    window.addEventListener("keydown", onKeyDown, true);
    return () => window.removeEventListener("keydown", onKeyDown, true);
  }, []);

  const listItem = userId === null ? null : (items.find((item) => item.id === userId) ?? null);

  return (
    <MotionConfig reducedMotion="user">
      <div className="h-full overflow-y-auto">
        <main className="px-4 pt-6 pb-28 lg:px-6">
          <PageHeader>
            <PageHeaderHeading>
              <PageHeaderTitle>用户管理</PageHeaderTitle>
            </PageHeaderHeading>
          </PageHeader>

          <div className="bg-card rounded-xl border">
            <div className="flex flex-wrap items-center gap-2 p-4">
              <Tooltip>
                <TooltipTrigger render={<div className="w-full sm:w-64" />}>
                  <SearchInput
                    ref={searchRef}
                    className="w-full"
                    aria-label="搜索用户名或邮箱"
                    placeholder="搜索用户名或邮箱"
                    autoComplete="off"
                    value={qInput}
                    onChange={(event) => setQInput(event.target.value)}
                    trailing={
                      qInput ? (
                        <button
                          type="button"
                          aria-label="清空搜索"
                          className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring/50 inline-flex size-5 items-center justify-center rounded outline-none focus-visible:ring-2"
                          onClick={() => {
                            setQInput("");
                            setQ("");
                            searchRef.current?.focus();
                          }}
                        >
                          <X className="size-3.5" />
                        </button>
                      ) : (
                        <span className="text-muted-foreground rounded border px-1 font-mono text-[11px]">
                          /
                        </span>
                      )
                    }
                  />
                </TooltipTrigger>
                <TooltipContent>搜索（/）</TooltipContent>
              </Tooltip>
              <Segmented aria-label="状态筛选">
                {STATUS_OPTIONS.map((option) => (
                  <SegmentedItem
                    key={option.value}
                    slideId="user-status-filter"
                    active={list.status === option.value}
                    onClick={() => list.setStatus(option.value)}
                  >
                    {option.label}
                  </SegmentedItem>
                ))}
              </Segmented>
              <NativeSelect
                aria-label="角色筛选"
                className="w-34"
                value={list.role}
                onChange={(event) => list.setRole(event.target.value as RoleFilter)}
              >
                {ROLE_OPTIONS.map((option) => (
                  <option key={option.value} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </NativeSelect>
              <span className="text-muted-foreground ml-auto flex items-center gap-2 text-xs tabular-nums">
                {list.refreshing && list.state !== "loading" && (
                  <Loader2 className="size-3.5 animate-spin" aria-label="加载中" />
                )}
                {list.state === "error"
                  ? ""
                  : list.filtering && list.overallTotal !== null
                    ? `筛出 ${list.total} / ${list.overallTotal} 人`
                    : `共 ${list.filtering ? list.total : (list.overallTotal ?? list.total)} 人`}
              </span>
            </div>

            <UserTable
              items={items}
              state={list.state}
              refreshing={list.refreshing}
              currentId={userId}
              actor={actor}
              actions={actions}
              selection={selection}
              filterText={list.q}
              filtering={list.filtering}
              onOpen={openUser}
              onRetry={list.reload}
              onClearFilters={list.clearFilters}
            />

            <div className="border-t py-3">
              <DataTablePagination
                page={page}
                pageSize={list.pageSize}
                total={list.total}
                onPageChange={setPage}
                onPageSizeChange={list.setPageSize}
              />
            </div>
          </div>
        </main>
      </div>

      <UserDialog
        userId={userId}
        order={pageIds}
        listItem={listItem}
        detail={detail.detail}
        detailState={detail.state}
        tab={list.tab}
        refreshKey={mutated}
        actor={actor}
        actions={actions}
        canPrev={canPrev}
        canNext={canNext}
        onStep={step}
        onTabChange={list.setTab}
        onClose={closeDialog}
        onRetry={reloadDetail}
      />

      <BulkBar
        users={selectedUsers}
        actor={actor}
        actions={actions}
        creditOpen={bulkCreditOpen}
        onCreditOpenChange={setBulkCreditOpen}
        onClear={clearSelection}
      />
    </MotionConfig>
  );
}
