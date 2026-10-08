import { Loader2 } from "lucide-react";
import {
  useEffect,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
  type RefObject,
} from "react";

import { MotionButton } from "@/components/admin-ui/motion-button";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Switch } from "@/components/ui/switch";
import { LIMIT_RANGE, validateLimit } from "@/utils/admin/user-rules";

import type { ActionUser, UserActions } from "./use-user-actions";

/**
 * 并发上限浮层：数字输入 + 「使用全局默认」开关。调低不会中断已在跑的任务，只影响新提交。
 * 触发器通过 render 传入；打开状态由调用方持有。
 */
function LimitPopover({
  user,
  actions,
  open,
  onOpenChange,
  render,
  children,
  anchor,
  align = "center",
}: {
  user: ActionUser;
  actions: UserActions;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 触发器元素；从行菜单打开时没有触发器，改用 anchor 指定弹出位置 */
  render?: ReactElement;
  children?: ReactNode;
  /** 没有触发器时，浮层对齐到这个元素 */
  anchor?: RefObject<HTMLElement | null>;
  align?: "start" | "center" | "end";
}) {
  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      {render && <PopoverTrigger render={render}>{children}</PopoverTrigger>}
      <PopoverContent
        anchor={anchor}
        align={align}
        sideOffset={6}
        className="w-[min(288px,calc(100vw-32px))] gap-0 p-3"
      >
        <LimitForm key={user.id} user={user} actions={actions} onDone={() => onOpenChange(false)} />
      </PopoverContent>
    </Popover>
  );
}

function LimitForm({
  user,
  actions,
  onDone,
}: {
  user: ActionUser;
  actions: UserActions;
  onDone: () => void;
}) {
  const [useDefault, setUseDefault] = useState(user.max_active_tasks === null);
  const [text, setText] = useState(
    user.max_active_tasks === null ? "" : String(user.max_active_tasks),
  );
  const inputRef = useRef<HTMLInputElement>(null);
  const busy = actions.isBusy(user.id, "limit");
  const check = validateLimit(text, useDefault);
  const showError = !useDefault && text !== "" && !check.valid;

  useEffect(() => {
    if (!useDefault) inputRef.current?.focus();
  }, [useDefault]);

  const submit = async () => {
    if (!check.valid || busy) return;
    if (await actions.setLimit(user, check.value)) onDone();
  };

  return (
    <div className="flex flex-col gap-2">
      <div className="text-sm font-medium">并发上限 · {user.username}</div>
      <p className="text-muted-foreground text-xs">
        调低不会中断已在跑的任务，只影响新提交。当前进行中{" "}
        <span className="tabular-nums">{user.active_tasks}</span> 个。
      </p>
      <label className="flex cursor-pointer items-center justify-between gap-2 rounded-md border px-3 py-2 text-sm">
        <span>
          使用全局默认
          {user.max_active_tasks === null && (
            <span className="tabular-nums">（{user.effective_max_active_tasks}）</span>
          )}
        </span>
        <Switch size="sm" checked={useDefault} disabled={busy} onCheckedChange={setUseDefault} />
      </label>
      <Input
        ref={inputRef}
        inputMode="numeric"
        autoComplete="off"
        maxLength={2}
        aria-label="并发上限"
        aria-invalid={showError}
        placeholder={`${LIMIT_RANGE.min}–${LIMIT_RANGE.max}`}
        value={text}
        disabled={useDefault || busy}
        className="font-mono tabular-nums"
        onChange={(event) => setText(event.target.value.replace(/\D/g, ""))}
        onKeyDown={(event) => {
          if (event.key !== "Enter") return;
          event.preventDefault();
          void submit();
        }}
      />
      <div className="mt-1 flex justify-end gap-2">
        <Button type="button" variant="ghost" size="xs" disabled={busy} onClick={onDone}>
          取消
        </Button>
        <MotionButton size="xs" disabled={!check.valid || busy} onClick={() => void submit()}>
          {busy && <Loader2 className="animate-spin" />}
          保存
        </MotionButton>
      </div>
    </div>
  );
}

export { LimitPopover };
