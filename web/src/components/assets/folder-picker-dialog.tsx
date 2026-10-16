import { useState, type FormEvent } from "react";
import { Check, Folder, FolderPlus, Home, Loader2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { useAssetFolders } from "@/hooks/use-asset-folders";
import { cn } from "@/lib/utils";
import { FOLDER_NAME_MAX, validateFolderName } from "@/utils/assets/folder-name";

/** 文件夹选择器的参数 */
export type FolderPickerProps = {
  /** 是否打开 */
  open: boolean;
  /** 打开状态变化 */
  onOpenChange: (open: boolean) => void;
  /** 标题，如「加入资产库」「移动到」 */
  title: string;
  /** 确认按钮文案，默认「确定」 */
  confirmLabel?: string;
  /** 打开时默认选中的位置；null 是根目录 */
  initialFolderId?: string | null;
  /** 当前所在的文件夹：标「当前位置」，选它时不能确认（移动场景）；根目录用 null，不传表示没有当前位置 */
  currentFolderId?: string | null;
  /** 确认：拿到选中的位置（null 是根目录）；失败抛错，弹窗保持打开 */
  onConfirm: (folderId: string | null) => Promise<unknown>;
};

/**
 * 文件夹选择器：根目录 + 各文件夹，可就地新建文件夹。
 * 资产页的「加入资产库…」「移动到…」用它，阶段二画布节点的「加入资产库」也复用同一个组件。
 * 每次打开都重新拉一遍文件夹（先显示旧数据），确认失败（请求层已弹提示）时保持打开。
 */
export function FolderPickerDialog(props: FolderPickerProps) {
  const { open, onOpenChange } = props;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        {/* 打开时才挂载内容：选中项、新建输入框每次都从初始状态开始 */}
        {open && <PickerBody {...props} />}
      </DialogContent>
    </Dialog>
  );
}

/** 一行位置：单选，选中显示对勾 */
function LocationRow({
  icon,
  name,
  hint,
  selected,
  onSelect,
}: {
  icon: React.ReactNode;
  name: string;
  hint?: string;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        "text-foreground flex h-10 w-full items-center gap-2.5 rounded-lg px-2.5 text-left text-sm transition-colors outline-none",
        "hover:bg-muted focus-visible:ring-ring/50 focus-visible:ring-3",
        selected && "bg-muted font-medium",
      )}
    >
      <span className="text-muted-foreground [&_svg]:size-4">{icon}</span>
      <span className="min-w-0 flex-1 truncate">{name}</span>
      {hint && <span className="text-muted-foreground shrink-0 text-xs tabular-nums">{hint}</span>}
      {selected && <Check className="size-4 shrink-0" />}
    </button>
  );
}

function PickerBody({
  onOpenChange,
  title,
  confirmLabel = "确定",
  initialFolderId = null,
  currentFolderId,
  onConfirm,
}: FolderPickerProps) {
  const { folders, loading, error, reload, create } = useAssetFolders(true);
  const [selected, setSelected] = useState<string | null>(initialFolderId);
  const [saving, setSaving] = useState(false);
  /** 就地新建：null 没展开，字符串是输入框里的内容 */
  const [draft, setDraft] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [draftError, setDraftError] = useState("");

  const isCurrent = currentFolderId !== undefined && selected === currentFolderId;

  const handleCreate = async (event: FormEvent) => {
    event.preventDefault();
    if (draft === null || creating) return;
    const result = validateFolderName(draft);
    if (!result.ok) {
      setDraftError(result.message);
      return;
    }
    setCreating(true);
    try {
      const folder = await create(result.name);
      setSelected(folder.id);
      setDraft(null);
      setDraftError("");
    } catch {
      // 重名、数量上限等业务错误由请求层弹出，输入框保持展开让用户改名
    } finally {
      setCreating(false);
    }
  };

  const handleConfirm = async () => {
    if (saving || isCurrent) return;
    setSaving(true);
    try {
      await onConfirm(selected);
      onOpenChange(false);
    } catch {
      // 提示由请求层统一弹，弹窗保持打开
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>选择一个位置，文件夹只有一层。</DialogDescription>
      </DialogHeader>

      <div
        role="radiogroup"
        aria-label="位置"
        className="-mx-1 grid max-h-72 gap-0.5 overflow-y-auto px-1"
      >
        <LocationRow
          icon={<Home />}
          name="根目录"
          hint={currentFolderId === null ? "当前位置" : undefined}
          selected={selected === null}
          onSelect={() => setSelected(null)}
        />
        {loading && (
          <div aria-busy className="grid gap-1 py-1">
            <Skeleton className="h-9 rounded-lg" />
            <Skeleton className="h-9 rounded-lg" />
          </div>
        )}
        {error && (
          <div className="text-muted-foreground flex items-center justify-between gap-2 px-2.5 py-2 text-[13px]">
            文件夹加载失败
            <Button variant="outline" size="xs" onClick={reload}>
              重试
            </Button>
          </div>
        )}
        {folders?.map((folder) => (
          <LocationRow
            key={folder.id}
            icon={<Folder />}
            name={folder.name}
            hint={folder.id === currentFolderId ? "当前位置" : `${folder.itemCount} 项`}
            selected={selected === folder.id}
            onSelect={() => setSelected(folder.id)}
          />
        ))}
      </div>

      {draft === null ? (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="text-muted-foreground justify-start"
          disabled={error || loading}
          onClick={() => setDraft("")}
        >
          <FolderPlus />
          新建文件夹
        </Button>
      ) : (
        <form onSubmit={handleCreate} className="grid gap-1.5">
          <div className="flex gap-2">
            <Input
              autoFocus
              value={draft}
              aria-label="新文件夹名称"
              aria-invalid={draftError !== ""}
              placeholder={`新文件夹名称（最多 ${FOLDER_NAME_MAX} 字）`}
              onChange={(event) => {
                setDraft(event.target.value);
                setDraftError("");
              }}
              onKeyDown={(event) => {
                // Esc 只收起输入框，不关闭整个弹窗
                if (event.key === "Escape") {
                  event.stopPropagation();
                  setDraft(null);
                  setDraftError("");
                }
              }}
            />
            <Button type="submit" variant="outline" disabled={creating}>
              {creating && <Loader2 className="animate-spin" />}
              创建
            </Button>
          </div>
          {draftError && (
            <span role="alert" className="text-destructive text-xs">
              {draftError}
            </span>
          )}
        </form>
      )}

      <DialogFooter>
        <Button
          type="button"
          variant="outline"
          onClick={() => onOpenChange(false)}
          disabled={saving}
        >
          取消
        </Button>
        <Button type="button" onClick={handleConfirm} disabled={saving || isCurrent}>
          {saving && <Loader2 className="animate-spin" />}
          {confirmLabel}
        </Button>
      </DialogFooter>
    </>
  );
}
