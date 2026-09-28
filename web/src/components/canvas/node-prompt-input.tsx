import type { ReactNode } from "react";
import { ArrowUp, ChevronDown, Loader2, Zap } from "lucide-react";

import { Button, buttonVariants } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";

/**
 * 输入框这块的尺寸，想调大调小改这里就够：
 * width 是整块的宽，minHeight / maxHeight 是文本区空着和写满时的高度。
 * 这块不随画布缩放，所以这些值就是实打实的屏幕像素（w-96 即 384px）。
 */
const PANEL_SIZE = {
  width: "w-96",
  minHeight: "min-h-16",
  maxHeight: "max-h-48",
} as const;

/** 一个可选模型：名字之外还带每次出片要扣的积分 */
export type NodeModelOption = {
  /** 模型标识，写回节点数据的就是它 */
  id: string;
  /** 模型显示名 */
  label: string;
  /** 单次消耗的积分 */
  credits: number;
  /** 下拉里的一行小字，说明擅长什么 */
  hint?: string;
};

type NodePromptInputProps = {
  /** 提示词文本 */
  value: string;
  onValueChange: (value: string) => void;
  /** 空输入时的提示 */
  placeholder?: string;
  /** 模型名左边的小图标 */
  icon?: ReactNode;
  /** 可选模型清单 */
  models: readonly NodeModelOption[];
  /** 当前选中的模型 id */
  modelId: string;
  onModelChange: (id: string) => void;
  /** 正在生成：发送按钮转圈并锁住 */
  running?: boolean;
  /** 不给就只存提示词，不跑生成 */
  onSubmit?: () => void;
  /** 没有 onSubmit 时，鼠标停在发送键上要给的说法 */
  submitHint?: string;
};

/**
 * 浮在节点下方的提示词输入框：一块自适应高度的文本区，
 * 底下一条工具栏放模型选择、积分和发送按钮。
 * 只管长相和交互，摆在哪儿、提示词与模型存哪儿都由调用方决定。
 */
export function NodePromptInput({
  value,
  onValueChange,
  placeholder = "写下你想要的内容",
  icon,
  models,
  modelId,
  onModelChange,
  running,
  onSubmit,
  submitHint = "这类节点还没接入生成服务",
}: NodePromptInputProps) {
  const model = models.find((item) => item.id === modelId) ?? models[0];
  const canSubmit = !!onSubmit && !running && value.trim().length > 0;

  const submit = () => {
    if (canSubmit) onSubmit?.();
  };

  // 按钮灰着的时候得说清为什么，不然只剩一个点不动的圈；
  // disabled 的按钮本身收不到鼠标事件，提示挂在外面那层上
  const hint = running
    ? "生成中"
    : !onSubmit
      ? submitHint
      : value.trim()
        ? "开始生成"
        : "先写点提示词";

  return (
    // 这块浮在节点外面，自带底色和阴影才压得住底下的画布；
    // nodrag 让框里能正常选字、点按钮，不会顺手把画布拖走
    <div
      className={cn(
        "nodrag border-input bg-card focus-within:border-ring focus-within:ring-ring/30 flex flex-col gap-3 rounded-2xl border p-3 text-left shadow-lg transition-[color,box-shadow] focus-within:ring-3 [corner-shape:squircle]",
        PANEL_SIZE.width,
      )}
    >
      <textarea
        value={value}
        placeholder={placeholder}
        // field-sizing-content 让文本区跟着内容长，长到上限再自己滚，
        // nowheel 把滚轮留给文本区，别让画布跟着平移
        className={cn(
          "nowheel placeholder:text-muted-foreground field-sizing-content w-full resize-none bg-transparent px-1 py-0.5 text-sm leading-6 outline-none",
          PANEL_SIZE.minHeight,
          PANEL_SIZE.maxHeight,
        )}
        onChange={(event) => onValueChange(event.target.value)}
        // Enter 直接发，换行留给 Shift+Enter；
        // 中文输入法选词时那一下 Enter 是在敲拼音框，isComposing 把它挡回去
        onKeyDown={(event) => {
          if (event.key !== "Enter" || event.shiftKey) return;
          if (event.nativeEvent.isComposing) return;
          event.preventDefault();
          submit();
        }}
      />
      <div className="flex items-center gap-2">
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger
            className={cn(
              buttonVariants({ variant: "ghost", size: "sm" }),
              "text-muted-foreground min-w-0 shrink gap-2 px-2",
            )}
            aria-label="选择模型"
          >
            {icon}
            <span className="min-w-0 truncate">{model.label}</span>
            <ChevronDown className="opacity-60" />
          </DropdownMenuTrigger>
          <DropdownMenuContent className="w-60" align="start" sideOffset={6}>
            <DropdownMenuGroup>
              {/* GroupLabel 必须待在 Group 里，否则 Base UI 会抛 MenuGroupContext 缺失 */}
              <DropdownMenuLabel>选择模型</DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuRadioGroup
                value={model.id}
                onValueChange={(next) => onModelChange(next as string)}
              >
                {models.map((item) => (
                  <DropdownMenuRadioItem key={item.id} value={item.id}>
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate">{item.label}</span>
                      {item.hint && (
                        <span className="text-muted-foreground truncate text-xs">
                          {item.hint}
                        </span>
                      )}
                    </span>
                    <span className="text-muted-foreground ml-auto text-xs">
                      {item.credits} 积分
                    </span>
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        <span
          className="text-muted-foreground ml-auto flex shrink-0 items-center gap-1 text-xs tabular-nums"
          title={`每次生成消耗 ${model.credits} 积分`}
        >
          <Zap className="size-3.5" />
          {model.credits}
        </span>
        <span className="flex shrink-0" title={hint}>
          <Button
            size="icon"
            className="rounded-full"
            aria-label={running ? "生成中" : "开始生成"}
            disabled={!canSubmit}
            onClick={submit}
          >
            {running ? <Loader2 className="animate-spin" /> : <ArrowUp />}
          </Button>
        </span>
      </div>
    </div>
  );
}
