import { useState, type FormEvent } from "react";
import { Loader2 } from "lucide-react";

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

/** 标题最长多少个字，与后端一致 */
const MAX_TITLE = 50;

/**
 * 重命名对话的小弹窗：标题 1–50 字，首尾空白会被去掉，空标题不能保存。
 * 保存失败（请求层已弹提示）时弹窗保持打开，用户可以再试。
 * @param open 是否打开
 * @param title 当前标题，打开时作为初始值
 * @param onClose 关闭
 * @param onSubmit 保存新标题，失败时抛错
 */
export function RenameDialog({
  open,
  title,
  onClose,
  onSubmit,
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  onSubmit: (title: string) => Promise<void>;
}) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-sm">
        {/* 打开时才挂载表单，初始值每次都取最新标题，不用在 effect 里同步 */}
        {open && <RenameForm initial={title} onClose={onClose} onSubmit={onSubmit} />}
      </DialogContent>
    </Dialog>
  );
}

function RenameForm({
  initial,
  onClose,
  onSubmit,
}: {
  initial: string;
  onClose: () => void;
  onSubmit: (title: string) => Promise<void>;
}) {
  const [value, setValue] = useState(initial);
  const [saving, setSaving] = useState(false);
  const trimmed = value.trim();

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    if (!trimmed || saving) return;
    setSaving(true);
    try {
      await onSubmit(trimmed);
      onClose();
    } catch {
      // 提示由请求层统一弹，弹窗保持打开
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} className="grid gap-4">
      <DialogHeader>
        <DialogTitle>重命名对话</DialogTitle>
        <DialogDescription>给这段对话起个好认的名字，最多 {MAX_TITLE} 个字。</DialogDescription>
      </DialogHeader>
      <Input
        autoFocus
        value={value}
        maxLength={MAX_TITLE}
        aria-label="对话标题"
        onChange={(event) => setValue(event.target.value)}
        onFocus={(event) => event.currentTarget.select()}
      />
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose} disabled={saving}>
          取消
        </Button>
        <Button type="submit" disabled={!trimmed || saving}>
          {saving && <Loader2 className="animate-spin" />}
          保存
        </Button>
      </DialogFooter>
    </form>
  );
}
