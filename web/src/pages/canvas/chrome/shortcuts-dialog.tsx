import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";

import { MOD } from "@/components/canvas-editor/keys";

const SHORTCUTS: [string, string][] = [
  ["发送生成", `${MOD}↵`],
  ["聚焦选中节点的提示词", "↵"],
  ["撤销 / 重做", `${MOD}Z / ⇧${MOD}Z`],
  ["立即保存", `${MOD}S`],
  ["复制 / 粘贴 / 原地复制", `${MOD}C / ${MOD}V / ${MOD}D`],
  ["打组 / 解组", `${MOD}G / ⇧${MOD}G`],
  ["给选中的组改名", "↵ / F2 / 双击组名"],
  ["删除选中", "⌫"],
  ["选择 / 抓手 / 临时抓手", "V / H / 空格"],
  ["放大 / 缩小", `${MOD}+ / ${MOD}−`],
  ["适应画布 / 100%", "⇧1 / ⇧0"],
  ["取消选中", "Esc"],
  ["打开快捷键", "?"],
];

const VIEW_SHORTCUTS = new Set(["放大 / 缩小", "适应画布 / 100%", "打开快捷键"]);

export function ShortcutsDialog({
  open,
  onOpenChange,
  scope = "personal",
  canGenerate = true,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  scope?: "personal" | "editor" | "viewer";
  canGenerate?: boolean;
}) {
  const shortcuts = SHORTCUTS.filter(([label]) =>
    scope === "viewer" ? VIEW_SHORTCUTS.has(label) : canGenerate || label !== "发送生成",
  );
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[min(88svh,620px)] flex-col sm:max-w-md">
        <DialogHeader>
          <DialogTitle>快捷键</DialogTitle>
        </DialogHeader>
        <dl className="grid min-h-0 grid-cols-[minmax(0,1fr)_minmax(0,auto)] items-center gap-x-3 gap-y-2.5 overflow-y-auto text-sm sm:gap-x-6">
          {shortcuts.map(([label, keys]) => (
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
