import { useCallback, useState } from "react";
import { toast } from "sonner";

import {
  adjustUserCredits,
  batchAddCredits,
  batchSetStatus,
  setUserLimit,
  setUserRole,
  setUserStatus,
} from "@/api/admin-users";
import type {
  AdjustCreditsRequest,
  UserDetail,
  UserListItem,
  UserRole,
  UserStatus,
} from "@/api/admin-users/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import {
  batchPlan,
  creditUndoRequest,
  USER_ROLE_LABEL,
  type Actor,
  type BatchUser,
} from "@/utils/admin/user-rules";

/** 撤销 toast 停留的时间（毫秒） */
const UNDO_MS = 5000;

/** 被操作的用户：行与抽屉都能给出这些字段 */
export type ActionUser = Pick<
  UserListItem,
  | "id"
  | "username"
  | "role"
  | "status"
  | "balance"
  | "frozen"
  | "available"
  | "max_active_tasks"
  | "effective_max_active_tasks"
  | "active_tasks"
>;

/**
 * 用户管理的全部写操作：行菜单、积分格、抽屉、批量条共用。
 * 请求失败的全局 toast 由拦截器弹，这里不重复，只在失败后保持界面状态（按钮恢复、浮层不关）。
 * 成功后用接口返回值原地更新（让数字滚动），再静默刷新列表与抽屉。
 * @param options.actor 当前管理员，批量跳过规则要用
 * @param options.patch 同时更新列表行与抽屉详情
 * @param options.refresh 静默重新拉取列表与抽屉详情
 */
export function useUserActions(options: {
  actor: Actor | null;
  patch: (id: number, patch: Partial<UserDetail>) => void;
  refresh: (id?: number) => void;
}) {
  const { actor, patch, refresh } = options;
  const [busy, setBusy] = useState<string | null>(null);

  /** 某个用户的某个动作是否在请求中 */
  const isBusy = useCallback((id: number, kind: string) => busy === `${id}:${kind}`, [busy]);

  /** 包一层 busy：同一时刻只有一个单用户请求，期间相关按钮转圈并禁用 */
  const run = useCallback(
    async <T>(key: string, task: () => Promise<T>): Promise<T | undefined> => {
      setBusy(key);
      try {
        return await task();
      } catch {
        // 全局 toast 已弹，调用方据 undefined 保持浮层打开
        return undefined;
      } finally {
        setBusy(null);
      }
    },
    [],
  );

  /**
   * 调整积分，成功后 toast 带 5 秒撤销（反向 add / sub，备注「撤销：原备注」）
   * @returns 是否成功；失败时浮层保持打开
   */
  const adjustCredits = useCallback(
    async (user: ActionUser, body: AdjustCreditsRequest): Promise<boolean> => {
      const before = user.available;
      const result = await run(`${user.id}:credit`, () => adjustUserCredits(user.id, body));
      if (!result) return false;
      patch(user.id, {
        balance: result.balance,
        frozen: result.frozen,
        available: result.available,
        has_credit_account: true,
      });
      refresh(user.id);
      const delta = result.available - before;
      const message =
        body.mode === "set"
          ? `已将 ${user.username} 的可用积分设为 ${result.available}`
          : delta >= 0
            ? `已为 ${user.username} 增加 ${delta} 积分`
            : `已为 ${user.username} 扣减 ${-delta} 积分`;
      const undo = creditUndoRequest(delta, body.note);
      toast.success(message, {
        duration: UNDO_MS,
        action: undo && {
          label: "撤销",
          onClick: () => {
            void adjustUserCredits(user.id, undo)
              .then((back) => {
                patch(user.id, {
                  balance: back.balance,
                  frozen: back.frozen,
                  available: back.available,
                });
                refresh(user.id);
                toast(`已撤销，${user.username} 可用积分恢复为 ${back.available}`);
              })
              .catch(() => undefined);
          },
        },
      });
      return true;
    },
    [patch, refresh, run],
  );

  /** 设置并发上限（null 为改回全局默认），toast 带撤销 */
  const setLimit = useCallback(
    async (user: ActionUser, value: number | null): Promise<boolean> => {
      const before = { max: user.max_active_tasks, effective: user.effective_max_active_tasks };
      const ok = await run(`${user.id}:limit`, async () => {
        await setUserLimit(user.id, { max_active_tasks: value });
        return true;
      });
      if (!ok) return false;
      patch(user.id, {
        max_active_tasks: value,
        ...(value !== null ? { effective_max_active_tasks: value } : {}),
      });
      refresh(user.id);
      toast.success(
        value === null
          ? `${user.username} 已改回全局默认并发`
          : `${user.username} 的并发上限已设为 ${value}`,
        {
          duration: UNDO_MS,
          action: {
            label: "撤销",
            onClick: () => {
              void setUserLimit(user.id, { max_active_tasks: before.max })
                .then(() => {
                  patch(user.id, {
                    max_active_tasks: before.max,
                    effective_max_active_tasks: before.effective,
                  });
                  refresh(user.id);
                })
                .catch(() => undefined);
            },
          },
        },
      );
      return true;
    },
    [patch, refresh, run],
  );

  /** 封禁 / 启用：可逆，直接执行并给 5 秒撤销，不弹确认框 */
  const setStatus = useCallback(
    async (user: ActionUser, to: UserStatus): Promise<boolean> => {
      const ok = await run(`${user.id}:status`, async () => {
        await setUserStatus(user.id, { status: to });
        return true;
      });
      if (!ok) return false;
      patch(user.id, { status: to });
      refresh(user.id);
      const back: UserStatus = to === "disabled" ? "active" : "disabled";
      toast.success(`已${to === "disabled" ? "封禁" : "启用"} ${user.username}`, {
        duration: UNDO_MS,
        action: {
          label: "撤销",
          onClick: () => {
            void setUserStatus(user.id, { status: back })
              .then(() => {
                patch(user.id, { status: back });
                refresh(user.id);
              })
              .catch(() => undefined);
          },
        },
      });
      return true;
    },
    [patch, refresh, run],
  );

  /** 封禁并取消进行中的任务：任务取消不可逆，所以先弹确认框，成功的 toast 不带撤销 */
  const banAndCancel = useCallback(
    async (user: ActionUser): Promise<boolean> => {
      const done = await confirm({
        title: `封禁 ${user.username} 并取消任务？`,
        destructive: true,
        confirmLabel: "封禁并取消任务",
        description: `将取消 ${user.active_tasks} 个进行中任务并退还 ${user.frozen} 冻结积分，此操作无法撤销。账号封禁之后可以再启用。`,
        onConfirm: () => setUserStatus(user.id, { status: "disabled", cancel_active: true }),
      });
      if (!done) return false;
      patch(user.id, { status: "disabled", active_tasks: 0 });
      refresh(user.id);
      toast.success(
        `已封禁 ${user.username}，取消 ${user.active_tasks} 个任务并退还 ${user.frozen} 积分`,
      );
      return true;
    },
    [patch, refresh],
  );

  /** 修改角色（仅 super_admin，后端会再判断） */
  const changeRole = useCallback(
    async (user: ActionUser, role: UserRole): Promise<boolean> => {
      const ok = await run(`${user.id}:role`, async () => {
        await setUserRole(user.id, role);
        return true;
      });
      if (!ok) return false;
      patch(user.id, { role });
      refresh(user.id);
      toast.success(`${user.username} 已改为「${USER_ROLE_LABEL[role]}」`);
      return true;
    },
    [patch, refresh, run],
  );

  /**
   * 批量封禁 / 启用：无权限的账号先在前端跳过，其余一次提交；toast 带撤销与「已跳过 N 个：原因」。
   * @returns 是否有账号被处理（调用方据此清除勾选）
   */
  const bulkStatus = useCallback(
    async (users: BatchUser[], to: UserStatus): Promise<boolean> => {
      const verb = to === "disabled" ? "封禁" : "启用";
      const plan = batchPlan(actor, users, to === "disabled" ? "ban" : "unban");
      if (plan.run.length === 0) {
        toast.info(
          plan.skipText ? `没有可${verb}的用户。${plan.skipText}` : `选中的用户都已${verb}`,
        );
        return false;
      }
      const result = await run("bulk:status", () =>
        batchSetStatus({ ids: plan.run.map((u) => u.id), status: to }),
      );
      if (!result) return false;
      const okIds = result.results.filter((r) => r.ok).map((r) => r.id);
      const failed = result.results.filter((r) => !r.ok);
      okIds.forEach((id) => patch(id, { status: to }));
      refresh();
      const parts = [
        `已${verb} ${okIds.length} 个用户`,
        plan.skipText,
        failed.length ? `失败 ${failed.length} 个：${failed[0]?.error ?? "未知原因"}` : "",
      ].filter(Boolean);
      const back: UserStatus = to === "disabled" ? "active" : "disabled";
      toast.success(parts.join("，"), {
        duration: UNDO_MS,
        action: okIds.length
          ? {
              label: "撤销",
              onClick: () => {
                void batchSetStatus({ ids: okIds, status: back })
                  .then(() => {
                    okIds.forEach((id) => patch(id, { status: back }));
                    refresh();
                  })
                  .catch(() => undefined);
              },
            }
          : undefined,
      });
      return okIds.length > 0;
    },
    [actor, patch, refresh, run],
  );

  /**
   * 批量发积分（只增不减）。批量接口只增不减，所以这里不提供一键撤销，要撤回请逐个扣减。
   * @returns 是否成功提交；失败时弹窗保持打开
   */
  const bulkCredits = useCallback(
    async (users: ActionUser[], amount: number, note: string): Promise<boolean> => {
      const plan = batchPlan(actor, users, "credit");
      if (plan.run.length === 0) {
        toast.info(plan.skipText || "没有可发放的用户");
        return false;
      }
      const result = await run("bulk:credit", () =>
        batchAddCredits({ ids: plan.run.map((u) => u.id), amount, note }),
      );
      if (!result) return false;
      const okIds = new Set(result.results.filter((r) => r.ok).map((r) => r.id));
      const failed = result.results.filter((r) => !r.ok);
      plan.run.forEach((u) => {
        if (okIds.has(u.id)) {
          patch(u.id, { available: u.available + amount, balance: u.balance + amount });
        }
      });
      refresh();
      const parts = [
        `已给 ${okIds.size} 个用户各发 ${amount} 积分`,
        plan.skipText,
        failed.length ? `失败 ${failed.length} 个：${failed[0]?.error ?? "未知原因"}` : "",
      ].filter(Boolean);
      toast.success(parts.join("，"));
      return true;
    },
    [actor, patch, refresh, run],
  );

  return {
    isBusy,
    busy,
    adjustCredits,
    setLimit,
    setStatus,
    banAndCancel,
    changeRole,
    bulkStatus,
    bulkCredits,
  };
}

/** 用户行 / 抽屉共用的动作集合 */
export type UserActions = ReturnType<typeof useUserActions>;
