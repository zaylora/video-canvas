import { Ellipsis } from "lucide-react";
import type { ComponentProps, ReactElement, ReactNode } from "react";

import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

/**
 * 行内图标按钮的提示：可用时 hover 显示操作名，被禁用时改为说明原因。
 * 包在外面的是 span，所以 children 可以是 Button，也可以是自带触发器的浮层（如并发上限浮层）。
 * @param label 操作名
 * @param deny 禁用原因；为空表示可用
 */
function RowHint({
  label,
  deny,
  children,
}: {
  label: string;
  deny?: string | null;
  children: ReactElement;
}) {
  if (deny) return <ReasonTooltip reason={deny}>{children}</ReasonTooltip>;
  return (
    <Tooltip>
      <TooltipTrigger render={<span className="inline-flex" />}>{children}</TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

/**
 * 表格行内的图标按钮（模型、渠道、插件、用户管理共用）：只有图标，hover 出操作名，
 * 禁用时 hover 出原因；危险操作用 destructive 色。点击不会冒泡到整行的点击。
 * @param label 操作名，hover 时显示
 * @param target 操作对象的名字（如模型名），拼在操作名后面作无障碍名称：「测试 GPT」
 * @param deny 禁用原因；有值时按钮禁用
 * @param destructive 危险操作（删除、封禁）
 */
function RowIconAction({
  label,
  target,
  deny,
  destructive,
  className,
  onClick,
  children,
  ...props
}: Omit<ComponentProps<typeof Button>, "variant" | "size" | "aria-label"> & {
  label: string;
  target?: string;
  deny?: string | null;
  destructive?: boolean;
}) {
  return (
    <RowHint label={label} deny={deny}>
      <Button
        data-slot="row-icon-action"
        variant="ghost"
        size="icon-sm"
        aria-label={target ? `${label} ${target}` : label}
        disabled={!!deny || props.disabled}
        className={cn(destructive && "text-destructive hover:text-destructive", className)}
        onClick={(event) => {
          event.stopPropagation();
          onClick?.(event);
        }}
        {...props}
      >
        {children}
      </Button>
    </RowHint>
  );
}

/**
 * 行末的「⋯」菜单：收纳不常用或有风险的操作。菜单项用 DropdownMenuItem 写在 children 里。
 * 触发按钮和菜单里的点击都不会冒泡到整行，免得点「更多操作」或菜单项时打开行的编辑弹窗。
 * @param label 触发按钮的无障碍名称，如「渠道 X 的更多操作」
 * @param className 菜单宽度等，默认 w-44
 */
function RowMoreMenu({
  label,
  className,
  children,
}: {
  label: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger
          render={
            <DropdownMenuTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={label}
                  onClick={(event) => event.stopPropagation()}
                />
              }
            />
          }
        >
          <Ellipsis />
        </TooltipTrigger>
        <TooltipContent>更多操作</TooltipContent>
      </Tooltip>
      <DropdownMenuContent
        align="end"
        className={cn("w-44", className)}
        onClick={(event) => event.stopPropagation()}
      >
        {children}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export { RowHint, RowIconAction, RowMoreMenu };
