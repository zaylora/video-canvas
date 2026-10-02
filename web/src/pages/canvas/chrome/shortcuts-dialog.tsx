import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";

import { MOD } from "./keys";

const SHORTCUTS: [string, string][] = [
  ["发送生成", `${MOD}↵`],
  ["聚焦选中节点的提示词", "↵"],
  ["撤销 / 重做", `${MOD}Z / ⇧${MOD}Z`],
  ["复制 / 粘贴 / 原地复制", `${MOD}C / ${MOD}V / ${MOD}D`],
  ["删除选中", "⌫"],
  ["选择 / 抓手 / 临时抓手", "V / H / 空格"],
  ["放大 / 缩小", `${MOD}+ / ${MOD}−`],
  ["适应画布 / 100%", "⇧1 / ⇧0"],
  ["取消选中", "Esc"],
  ["打开快捷键", "?"],
];

export function ShortcutsDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>快捷键</DialogTitle>
        </DialogHeader>
        <dl className="grid grid-cols-[1fr_auto] items-center gap-x-6 gap-y-2.5 text-sm">
          {SHORTCUTS.map(([label, keys]) => (
            <div key={label} className="contents">
              <dt className="text-muted-foreground">{label}</dt>
              <dd>
                <kbd className="bg-muted rounded-md px-2 py-0.5 font-mono text-xs">{keys}</kbd>
              </dd>
            </div>
          ))}
        </dl>
      </DialogContent>
    </Dialog>
  );
}
