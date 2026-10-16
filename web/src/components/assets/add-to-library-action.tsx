import { useState } from "react";
import { Check, FolderPlus } from "lucide-react";

import { FolderPickerDialog } from "./folder-picker-dialog";

/**
 * 预览灯箱底部的「加入资产库」按钮：点开文件夹选择器。
 * 选择器必须渲染在灯箱的组件树里（而不是页面层），Base UI 才会把它当作嵌套弹层，焦点和 Esc 才正确。
 * 已在资产库的素材显示一个不可点的「已在资产库」。
 * @param inLibrary 素材是否已在资产库
 * @param onAdd 确认加入：拿到目标文件夹（null 是根目录）；失败抛错
 */
export function AddToLibraryAction({
  inLibrary,
  onAdd,
}: {
  inLibrary: boolean;
  onAdd: (folderId: string | null) => Promise<unknown>;
}) {
  const [open, setOpen] = useState(false);
  const chip =
    "bg-chrome ring-chrome-border text-foreground flex h-7 items-center gap-1.5 rounded-full px-3 ring-1";

  if (inLibrary) {
    return (
      <span className={`${chip} text-muted-foreground`}>
        <Check className="size-3.5" />
        已在资产库
      </span>
    );
  }
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className={`${chip} hover:bg-chrome-hover focus-visible:ring-node-ring transition-colors outline-none focus-visible:ring-2`}
      >
        <FolderPlus className="size-3.5" />
        加入资产库…
      </button>
      <FolderPickerDialog
        open={open}
        onOpenChange={setOpen}
        title="加入资产库"
        confirmLabel="加入"
        onConfirm={onAdd}
      />
    </>
  );
}
