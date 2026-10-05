import { useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

import { replaceSmtpPassword } from "@/api/admin/settings";
import { FormField } from "@/components/admin-ui/form-field";
import { Notice } from "@/components/admin-ui/notice";
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

import { useAliveRef } from "../use-admin";

/**
 * 替换 SMTP 密码（仅 super_admin）。密码永远只写：
 * - 输入框从空白开始，type=password、autocomplete=new-password；
 * - 明文只存在于对话框 state 里，提交时取走并立刻清空，不进 URL、store、日志。
 * @param open 是否打开
 * @param onClose 关闭
 * @param onReplaced 替换成功后回调（让页面刷新 has_password）
 */
export function SmtpPasswordDialog({
  open,
  onClose,
  onReplaced,
}: {
  open: boolean;
  onClose: () => void;
  onReplaced: () => void;
}) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-md">
        <PasswordBody onClose={onClose} onReplaced={onReplaced} />
      </DialogContent>
    </Dialog>
  );
}

function PasswordBody({ onClose, onReplaced }: { onClose: () => void; onReplaced: () => void }) {
  const aliveRef = useAliveRef();
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const ready = password.trim() !== "";

  const submit = async () => {
    if (!ready || busy) return;
    const body = { password };
    setPassword("");
    setBusy(true);
    try {
      await replaceSmtpPassword(body);
      if (!aliveRef.current) return;
      toast.success("SMTP 密码已替换");
      onReplaced();
      onClose();
    } catch {
      // 全局 toast 已弹；原密码不受影响，需要重新输入
    } finally {
      if (aliveRef.current) setBusy(false);
    }
  };

  return (
    <form
      className="contents"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <DialogHeader>
        <DialogTitle>替换密码</DialogTitle>
        <DialogDescription>新密码保存后无法再查看，只能再次替换。</DialogDescription>
      </DialogHeader>
      <FormField label="SMTP 密码" htmlFor="smtp-new-password" required>
        <Input
          id="smtp-new-password"
          type="password"
          className="font-mono"
          autoComplete="new-password"
          spellCheck={false}
          autoFocus
          value={password}
          disabled={busy}
          onChange={(event) => setPassword(event.target.value)}
        />
      </FormField>
      <Notice tone="info">密码加密存储，需要服务端配置 APP_AI_SECRET_KEY。</Notice>
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onClose}>
          取消
        </Button>
        <Button type="submit" disabled={!ready || busy}>
          {busy && <Loader2 className="animate-spin" />}
          替换
        </Button>
      </DialogFooter>
    </form>
  );
}
