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
import { FOLDER_NAME_MAX, validateFolderName } from "@/utils/assets/folder-name";

/**
 * 新建 / 重命名文件夹的小弹窗：名称去首尾空白后 1–30 字。
 * 本地只校验长度；重名、数量上限由后端判断，业务错误文案由请求层弹出，弹窗保持打开让用户改名重试。
 * @param open 是否打开
 * @param mode create 新建，rename 重命名
 * @param initial 重命名时的当前名称，新建传空串；打开时作为初始值
 * @param onClose 关闭
 * @param onSubmit 提交名称，失败时抛错
 */
export function FolderNameDialog({
  open,
  mode,
  initial = "",
  onClose,
  onSubmit,
}: {
  open: boolean;
  mode: "create" | "rename";
  initial?: string;
  onClose: () => void;
  onSubmit: (name: string) => Promise<unknown>;
}) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-sm">
        {/* 打开时才挂载表单，初始值每次都取最新名称，不用在 effect 里同步 */}
        {open && (
          <FolderNameForm mode={mode} initial={initial} onClose={onClose} onSubmit={onSubmit} />
        )}
      </DialogContent>
    </Dialog>
  );
}

function FolderNameForm({
  mode,
  initial,
  onClose,
  onSubmit,
}: {
  mode: "create" | "rename";
  initial: string;
  onClose: () => void;
  onSubmit: (name: string) => Promise<unknown>;
}) {
  const [value, setValue] = useState(initial);
  const [saving, setSaving] = useState(false);
  /** 提交过一次才显示校验提示，输入过程中不抢着报错 */
  const [touched, setTouched] = useState(false);
  const result = validateFolderName(value);
  const invalid = !result.ok;

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setTouched(true);
    if (!result.ok || saving) return;
    setSaving(true);
    try {
      await onSubmit(result.name);
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
        <DialogTitle>{mode === "create" ? "新建文件夹" : "重命名文件夹"}</DialogTitle>
        <DialogDescription>
          名称最多 {FOLDER_NAME_MAX} 个字，不能与已有文件夹重名。
        </DialogDescription>
      </DialogHeader>
      <div className="grid gap-1.5">
        <Input
          autoFocus
          value={value}
          aria-label="文件夹名称"
          aria-invalid={touched && invalid}
          placeholder="例如：角色图"
          onChange={(event) => setValue(event.target.value)}
          onFocus={(event) => event.currentTarget.select()}
        />
        {touched && !result.ok && (
          <span role="alert" className="text-destructive text-xs">
            {result.message}
          </span>
        )}
      </div>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose} disabled={saving}>
          取消
        </Button>
        <Button type="submit" disabled={saving}>
          {saving && <Loader2 className="animate-spin" />}
          {mode === "create" ? "创建" : "保存"}
        </Button>
      </DialogFooter>
    </form>
  );
}
