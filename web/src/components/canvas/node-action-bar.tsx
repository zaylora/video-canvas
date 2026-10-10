import type { CSSProperties } from "react";
import { ChevronDown, Copy, Download, History, Maximize2, Sparkle } from "lucide-react";
import { toast } from "sonner";

import {
  ChromeButton,
  ChromePill,
  ChromeSeparator,
  ChromeTooltip,
} from "@/components/canvas/chrome/chrome";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import type { NodeToolbarConfig, ToolbarAction, ToolbarTail } from "@/constants/node-toolbar";
import { DURATION, EASE_OUT_CSS, ms } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { NodeOutput } from "@/types";
import type { ToolbarGate } from "@/utils/canvas/node-toolbar-state";

import { HistoryThumbs } from "./node-history-strip";

/**
 * 弹层往上渐显：只做透明度和向上浮 6px，不缩放（设计稿 6.16 动效表）。
 * shadcn 菜单默认是向下滑加 zoom-in-95，这里用 tw-animate 的变量压过去；
 * 进入 DURATION.base，退出 DURATION.exit，减少动态效果时只剩淡入淡出。
 */
const POPUP_MOTION_CLASS = cn(
  "data-open:[--tw-enter-scale:1]! data-open:[--tw-enter-translate-y:6px]!",
  "data-closed:[--tw-exit-scale:1]! data-closed:[--tw-exit-translate-y:6px]!",
  "data-open:duration-(--bar-in) data-closed:duration-(--bar-out) ease-(--bar-ease)",
  "motion-reduce:data-open:[--tw-enter-translate-y:0px]! motion-reduce:data-closed:[--tw-exit-translate-y:0px]!",
);
const POPUP_MOTION_STYLE = {
  "--bar-fast": ms(DURATION.fast),
  "--bar-in": ms(DURATION.base),
  "--bar-out": ms(DURATION.exit),
  "--bar-ease": EASE_OUT_CSS,
} as CSSProperties;

/** 文字按钮：悬停变底色，菜单展开时保持高亮、箭头翻转 */
const TEXT_BUTTON_CLASS =
  "group/bar text-foreground px-2.5 max-md:px-2 data-popup-open:bg-chrome-hover";

/** 图标按钮：默认灰，悬停变亮 */
const ICON_BUTTON_CLASS = "data-popup-open:bg-chrome-hover data-popup-open:text-foreground";

/** 占位功能点了之后的提示：不报错，不改节点数据 */
const comingSoon = (label: string) => toast(`「${label}」敬请期待`);

/** 名字后的积分星：这个功能会消耗积分 */
function PaidMark() {
  return (
    <Sparkle
      role="img"
      aria-label="消耗积分"
      className="text-credit size-3! fill-current stroke-0 max-md:hidden"
    />
  );
}

/** 一个功能按钮的内容：图标、文字（窄屏收起）、积分星、下拉箭头 */
function ActionBody({ action }: { action: ToolbarAction }) {
  return (
    <>
      <action.icon />
      <span className="max-md:hidden">{action.label}</span>
      {action.paid && <PaidMark />}
      {action.menu && (
        <ChevronDown className="size-3.5! opacity-60 transition-transform duration-(--bar-fast) ease-(--bar-ease) group-data-popup-open/bar:rotate-180" />
      )}
    </>
  );
}

/** 带下拉的功能按钮 */
function MenuAction({
  action,
  disabled,
  onSelect,
}: {
  action: ToolbarAction;
  disabled: boolean;
  onSelect: (id: string, label: string) => void;
}) {
  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger
        disabled={disabled}
        render={
          <ChromeButton
            aria-label={action.label}
            title={action.label}
            className={TEXT_BUTTON_CLASS}
            style={POPUP_MOTION_STYLE}
          />
        }
      >
        <ActionBody action={action} />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        side="bottom"
        align="start"
        sideOffset={8}
        className={cn("w-auto min-w-44 rounded-xl", POPUP_MOTION_CLASS)}
        style={POPUP_MOTION_STYLE}
      >
        {action.menu?.map((group, index) => (
          <DropdownMenuGroup key={group.label ?? index}>
            {group.label && <DropdownMenuLabel>{group.label}</DropdownMenuLabel>}
            {group.items.map((item) => (
              <DropdownMenuItem
                key={item.id}
                className="h-9 gap-2.5"
                onClick={() => onSelect(item.id, item.label)}
              >
                <item.icon className="text-muted-foreground" />
                {item.label}
                {item.paid && <PaidMark />}
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** 节点生成历史按钮：点开向下展开缩略图条，和其它菜单一个出场方式 */
function HistoryAction({
  nodeId,
  outputs,
  activeId,
  count,
  disabled,
  onSelect,
}: {
  nodeId: string;
  outputs: NodeOutput[];
  activeId?: string;
  count: number;
  disabled: boolean;
  onSelect: (outputId: string) => void;
}) {
  return (
    <Popover>
      <PopoverTrigger
        disabled={disabled}
        render={
          <ChromeButton
            aria-label="节点生成历史"
            title={count === 0 ? "生成后，每一版结果都会留在这里" : "节点生成历史"}
            className={ICON_BUTTON_CLASS}
            style={POPUP_MOTION_STYLE}
          />
        }
      >
        <History />
        {count > 1 && (
          <span className="bg-foreground text-background rounded-full px-1.5 font-mono text-[11px] leading-4 tabular-nums">
            {count}
          </span>
        )}
      </PopoverTrigger>
      <PopoverContent
        side="bottom"
        align="end"
        sideOffset={8}
        className={cn("nodrag nowheel nopan w-auto gap-1.5 rounded-xl p-2", POPUP_MOTION_CLASS)}
        style={POPUP_MOTION_STYLE}
      >
        <HistoryThumbs nodeId={nodeId} outputs={outputs} activeId={activeId} onSelect={onSelect} />
        <p className="text-muted-foreground px-0.5 text-xs">
          点缩略图切换版本 · 拖到画布上新建节点
        </p>
      </PopoverContent>
    </Popover>
  );
}

/** 查看类按钮的图标、名字和提示 */
const TAIL_META = {
  zoom: { icon: Maximize2, label: "放大预览" },
  download: { icon: Download, label: "下载" },
  copy: { icon: Copy, label: "复制文本" },
} as const;

/**
 * 节点功能区（设计稿 6.16）：选中节点时浮在标题行上方的一条胶囊，所有生成节点共用。
 * 按钮有哪些由 NODE_TOOLBAR 配置决定；左边是加工类功能（带下拉的展开菜单），
 * 分隔线右边是历史、放大、下载、复制这类查看类按钮。
 * 加工类功能这一版大多还是占位：没有给处理函数的功能点了只提示「敬请期待」。
 */
export function NodeActionBar({
  config,
  label,
  gate,
  history,
  onAction,
  onTail,
}: {
  config: NodeToolbarConfig;
  /** 节点标题，给读屏用 */
  label: string;
  gate: ToolbarGate;
  /** 历史按钮要用的版本数据；节点没有历史（文本）时不传 */
  history?: {
    nodeId: string;
    outputs: NodeOutput[];
    activeId?: string;
    onSelect: (outputId: string) => void;
  };
  /** 加工类功能和菜单项的处理函数，key 是配置里的 id；没有就当占位 */
  onAction?: Partial<Record<string, () => void>>;
  /** 放大、下载、复制的处理函数；没有就当占位 */
  onTail?: Partial<Record<Exclude<ToolbarTail, "history">, () => void>>;
}) {
  const run = (id: string, name: string) => {
    const handler = onAction?.[id];
    if (handler) handler();
    else comingSoon(name);
  };

  const tail = config.tail.map((item) => {
    if (item === "history") {
      if (!history) return null;
      return (
        <HistoryAction
          key={item}
          nodeId={history.nodeId}
          outputs={history.outputs}
          activeId={history.activeId}
          count={gate.historyCount}
          disabled={gate.historyDisabled}
          onSelect={history.onSelect}
        />
      );
    }
    const meta = TAIL_META[item];
    return (
      <ChromeTooltip key={item} label={meta.label}>
        <ChromeButton
          aria-label={meta.label}
          disabled={gate.actionsDisabled}
          onClick={() => (onTail?.[item] ?? (() => comingSoon(meta.label)))()}
        >
          <meta.icon />
        </ChromeButton>
      </ChromeTooltip>
    );
  });
  const hasTail = tail.some(Boolean);

  return (
    <ChromePill
      role="toolbar"
      aria-label={`${label} 的功能区`}
      className="nodrag nopan max-w-[calc(100vw-1.5rem)] gap-0.5"
    >
      {config.actions.map((action) =>
        action.menu ? (
          <MenuAction
            key={action.id}
            action={action}
            disabled={gate.actionsDisabled}
            onSelect={run}
          />
        ) : (
          <ChromeButton
            key={action.id}
            aria-label={action.label}
            title={action.label}
            disabled={gate.actionsDisabled}
            className={TEXT_BUTTON_CLASS}
            onClick={() => run(action.id, action.label)}
          >
            <ActionBody action={action} />
          </ChromeButton>
        ),
      )}
      {config.actions.length > 0 && hasTail && <ChromeSeparator />}
      {tail}
    </ChromePill>
  );
}
