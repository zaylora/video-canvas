import {
  Ban,
  CircleCheck,
  Ellipsis,
  Gauge,
  KeyRound,
  OctagonX,
  PanelRightOpen,
  Shield,
} from "lucide-react";
import { type ReactNode } from "react";

import { resetUserPassword } from "@/api/admin/users";
import type { UserRole } from "@/api/admin/users/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { CopyButton } from "@/components/admin-ui/copy-button";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { openDialog, type DialogControl } from "@/store/dialog";
import { denyReason, USER_ROLE_LABEL, type Actor } from "@/utils/admin/user-rules";

import type { ActionUser, UserActions } from "./use-user-actions";

const ROLES: UserRole[] = ["user", "admin", "super_admin"];

/** 重置密码后展示临时密码：只显示这一次，关闭后无法再查看 */
function TempPasswordDialog({
  username,
  password,
  open,
  onClose,
  onExited,
}: { username: string; password: string } & DialogControl) {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && onClose()}
      onOpenChangeComplete={(next) => !next && onExited()}
    >
      <DialogContent showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{username} 的临时密码</DialogTitle>
          <DialogDescription>
            只显示这一次，关闭后无法再查看。请通过安全渠道发给用户，并提醒对方登录后修改。
          </DialogDescription>
        </DialogHeader>
        <div className="bg-muted flex items-center gap-2 rounded-lg border px-3 py-2.5">
          <code className="flex-1 font-mono text-base tabular-nums select-all">{password}</code>
          <CopyButton text={password} label="复制临时密码" />
        </div>
        <DialogFooter>
          <Button onClick={onClose}>我已记录</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** 重置密码：先确认，成功后弹出临时密码（后端会让该用户已登录的设备立即失效） */
async function requestResetPassword(user: ActionUser) {
  let password = "";
  const ok = await confirm({
    title: `重置 ${user.username} 的密码？`,
    description: "将生成一个临时密码，该用户所有已登录的设备会立即退出。",
    confirmLabel: "生成临时密码",
    onConfirm: async () => {
      password = (await resetUserPassword(user.id)).temp_password;
    },
  });
  if (ok && password) openDialog(TempPasswordDialog, { username: user.username, password });
}

/**
 * 行菜单与抽屉「⋯」共用的菜单项。无权限的项禁用而不是隐藏，hover 用 ReasonTooltip 写原因；
 * 「封禁并取消 N 个任务」只在有进行中任务时出现。
 * @param variant row 是行菜单（含查看详情、并发、封禁 / 启用），sheet 是抽屉里的「⋯」（这些已在操作行上，只留危险项）
 */
function UserMenuItems({
  user,
  actor,
  actions,
  variant,
  onOpen,
  onAdjustLimit,
}: {
  user: ActionUser;
  actor: Actor | null;
  actions: UserActions;
  variant: "row" | "sheet";
  onOpen?: () => void;
  onAdjustLimit?: () => void;
}) {
  const banDeny = denyReason(actor, user, "ban");
  const limitDeny = denyReason(actor, user, "limit");
  const roleDeny = denyReason(actor, user, "role");
  const resetDeny = denyReason(actor, user, "reset");
  const disabled = user.status === "disabled";
  const canCancel = !disabled && user.active_tasks > 0;
  const row = variant === "row";

  const guarded = (reason: string | null, node: ReactNode) => (
    <ReasonTooltip reason={reason} className="block w-full">
      <div className="w-full">{node}</div>
    </ReasonTooltip>
  );

  return (
    <>
      {row && (
        <DropdownMenuGroup>
          <DropdownMenuItem onClick={onOpen}>
            <PanelRightOpen />
            查看详情
            <DropdownMenuShortcut>⏎</DropdownMenuShortcut>
          </DropdownMenuItem>
          {guarded(
            limitDeny,
            <DropdownMenuItem disabled={!!limitDeny} onClick={onAdjustLimit}>
              <Gauge />
              调整并发上限…
            </DropdownMenuItem>,
          )}
        </DropdownMenuGroup>
      )}
      {row && <DropdownMenuSeparator />}
      <DropdownMenuGroup>
        {row &&
          guarded(
            banDeny,
            disabled ? (
              <DropdownMenuItem
                disabled={!!banDeny}
                onClick={() => void actions.setStatus(user, "active")}
              >
                <CircleCheck />
                启用
              </DropdownMenuItem>
            ) : (
              <DropdownMenuItem
                variant="destructive"
                disabled={!!banDeny}
                onClick={() => void actions.setStatus(user, "disabled")}
              >
                <Ban />
                封禁
              </DropdownMenuItem>
            ),
          )}
        {canCancel &&
          guarded(
            banDeny,
            <DropdownMenuItem
              variant="destructive"
              disabled={!!banDeny}
              onClick={() => void actions.banAndCancel(user)}
            >
              <OctagonX />
              封禁并取消 {user.active_tasks} 个任务…
            </DropdownMenuItem>,
          )}
      </DropdownMenuGroup>
      {(row || canCancel) && <DropdownMenuSeparator />}
      <DropdownMenuGroup>
        {guarded(
          roleDeny,
          <DropdownMenuSub>
            <DropdownMenuSubTrigger disabled={!!roleDeny}>
              <Shield />
              修改角色
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="min-w-36">
              <DropdownMenuRadioGroup
                value={user.role}
                onValueChange={(value) => {
                  if (value !== user.role) void actions.changeRole(user, value as UserRole);
                }}
              >
                {ROLES.map((role) => (
                  <DropdownMenuRadioItem key={role} value={role}>
                    {USER_ROLE_LABEL[role]}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuSubContent>
          </DropdownMenuSub>,
        )}
        {guarded(
          resetDeny,
          <DropdownMenuItem disabled={!!resetDeny} onClick={() => void requestResetPassword(user)}>
            <KeyRound />
            重置密码…
          </DropdownMenuItem>,
        )}
      </DropdownMenuGroup>
    </>
  );
}

/**
 * 「⋯」菜单按钮 + 菜单。row 用在表格行末，sheet 用在抽屉操作行。
 * 触发按钮的 ref 交给调用方：从菜单打开并发浮层时，浮层要对齐到这个按钮。
 */
function UserRowMenu({
  user,
  actor,
  actions,
  variant = "row",
  triggerRef,
  onOpen,
  onAdjustLimit,
}: {
  user: ActionUser;
  actor: Actor | null;
  actions: UserActions;
  variant?: "row" | "sheet";
  triggerRef?: React.Ref<HTMLButtonElement>;
  onOpen?: () => void;
  onAdjustLimit?: () => void;
}) {
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger
          render={
            <DropdownMenuTrigger
              ref={triggerRef}
              render={
                <Button variant="ghost" size="icon-sm" aria-label={`${user.username} 的更多操作`} />
              }
            />
          }
        >
          <Ellipsis />
        </TooltipTrigger>
        <TooltipContent>更多操作</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end" className="w-60">
        <UserMenuItems
          user={user}
          actor={actor}
          actions={actions}
          variant={variant}
          onOpen={onOpen}
          onAdjustLimit={onAdjustLimit}
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export { UserRowMenu };
