import { useState } from "react";
import { Cpu, Frame, Plug, SlidersHorizontal } from "lucide-react";
import { cn } from "cn";

import { Button } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { isRemoteModelKind } from "@/constants/canvas";
import { useSettingsStore } from "@/store";

import { CanvasPanel } from "./canvas-panel";
import { CustomModelPanel } from "./custom-model-panel";
import { GeneralPanel } from "./general-panel";
import { ModelPanel, type SettingModelGroup } from "./model-panel";

const SECTIONS = [
  {
    value: "canvas",
    label: "画布设置",
    description: "底纹、对齐与滚轮，改动即时生效。",
    icon: Frame,
  },
  {
    value: "general",
    label: "通用设置",
    description: "外观与界面上的常驻提示。",
    icon: SlidersHorizontal,
  },
  {
    value: "model",
    label: "模型设置",
    description: "新建节点时默认选中的模型。",
    icon: Cpu,
  },
  {
    value: "custom",
    label: "自定义模型",
    description: "接入自己的服务，接好后出现在节点的模型下拉里。",
    icon: Plug,
  },
] as const;

type SectionValue = (typeof SECTIONS)[number]["value"];

type SettingsDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** 模型那两栏要列的种类与内置清单 */
  modelGroups: readonly SettingModelGroup[];
};

/** 左边分栏、右边内容的设置弹窗 */
export function SettingsDialog({
  open,
  onOpenChange,
  modelGroups,
}: SettingsDialogProps) {
  const resetSettings = useSettingsStore((state) => state.resetSettings);
  const [section, setSection] = useState<SectionValue>("canvas");
  // 服务端下发清单的种类由平台统一托管，不支持自定义模型；一个能自定义的种类都没有时整栏不显示
  const customKinds = modelGroups.filter((group) => !isRemoteModelKind(group.kind));
  const sections = SECTIONS.filter(
    (item) => item.value !== "custom" || customKinds.length > 0,
  );
  const active = sections.find((item) => item.value === section) ?? sections[0];

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* 内容区自己排版，所以把 DialogContent 默认的内边距和栅格让出来 */}
      <DialogContent className="gap-0 overflow-hidden p-0 sm:max-w-4xl">
        <div className="flex h-[min(34rem,80dvh)] min-h-0">
          <nav className="bg-muted/40 flex w-32 shrink-0 flex-col gap-2 border-r p-3 sm:w-48">
            {sections.map((item) => {
              const selected = item.value === section;

              return (
                <Button
                  key={item.value}
                  variant="ghost"
                  aria-pressed={selected}
                  className={cn(
                    "text-muted-foreground h-11 justify-start gap-2 rounded-lg px-2 text-sm font-medium sm:px-3",
                    selected && "bg-background text-foreground shadow-sm",
                  )}
                  onClick={() => setSection(item.value)}
                >
                  <item.icon className="size-4" />
                  <span className="truncate">{item.label}</span>
                </Button>
              );
            })}
          </nav>

          <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-5 p-6">
            <DialogHeader className="shrink-0">
              {/* 关闭按钮压在右上角，标题这行留出位置别顶上去 */}
              <DialogTitle className="pr-8">{active.label}</DialogTitle>
              <DialogDescription>{active.description}</DialogDescription>
            </DialogHeader>

            {/* 自定义模型那栏内容会长过弹窗，超出就自己滚；横向不留滚动条 */}
            <ScrollArea className="min-h-0 flex-1">
              <div className="flex flex-col gap-5 py-1 pr-4 pl-1">
                {section === "canvas" && <CanvasPanel />}
                {section === "general" && <GeneralPanel />}
                {section === "model" && <ModelPanel groups={modelGroups} />}
                {section === "custom" && (
                  <CustomModelPanel kinds={customKinds} />
                )}
              </div>
            </ScrollArea>

            <div className="flex shrink-0 justify-end">
              <Button variant="ghost" size="sm" onClick={resetSettings}>
                恢复默认
              </Button>
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
