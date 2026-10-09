import { useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

import { replaceStorageSecret } from "@/api/admin/storage";
import type { CloudProvider, StorageView } from "@/api/admin/storage/type.d";
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
import { PROVIDER_META } from "./provider-meta";

/**
 * 替换存储凭证（仅 super_admin）。Secret 永远只写：
 * - 输入框从空白开始，type=password、autocomplete=new-password；
 * - 明文只存在于对话框 state 里，提交时取走并立刻清空，不进 URL、store、日志；
 * - AccessKey ID 与 Secret 必须一起换；后端先用新凭证测试，不通过就什么都不改，原凭证继续可用。
 * 在弹窗里是受控的子弹窗（不走全局弹窗 store），这样 Esc 只关它自己。
 * @param storage 要替换凭证的存储
 * @param onReplaced 替换成功后，带回更新后的视图
 */
export function SecretDialog({
  open,
  storage,
  onClose,
  onReplaced,
}: {
  open: boolean;
  storage: StorageView;
  onClose: () => void;
  onReplaced: (view: StorageView) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-md">
        <SecretBody storage={storage} onClose={onClose} onReplaced={onReplaced} />
      </DialogContent>
    </Dialog>
  );
}

function SecretBody({
  storage,
  onClose,
  onReplaced,
}: {
  storage: StorageView;
  onClose: () => void;
  onReplaced: (view: StorageView) => void;
}) {
  const aliveRef = useAliveRef();
  const meta = PROVIDER_META[storage.provider as CloudProvider];
  const [accessKeyId, setAccessKeyId] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [busy, setBusy] = useState(false);
  const ready = accessKeyId.trim() !== "" && secretKey.trim() !== "";

  const submit = async () => {
    if (!ready || busy) return;
    // 取走明文并立刻清空输入：请求期间与之后 state 里都不再持有 Secret
    const body = { access_key_id: accessKeyId.trim(), secret_key: secretKey.trim() };
    setSecretKey("");
    setBusy(true);
    try {
      const view = await replaceStorageSecret(storage.id, body);
      if (!aliveRef.current) return;
      toast.success("凭证已替换，新凭证测试通过");
      onReplaced(view);
      onClose();
    } catch {
      // 全局 toast 已弹；原凭证不受影响，保留 AccessKey ID，Secret 需要重新输入
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
        <DialogTitle>替换凭证</DialogTitle>
        <DialogDescription>
          存储 <b>{storage.name}</b>。AccessKey ID 与 Secret 必须一起更换，Secret 只写不读。
        </DialogDescription>
      </DialogHeader>
      <FormField label={meta.keyLabel} htmlFor="storage-new-key" required>
        <Input
          id="storage-new-key"
          className="font-mono"
          autoComplete="off"
          spellCheck={false}
          value={accessKeyId}
          disabled={busy}
          onChange={(event) => setAccessKeyId(event.target.value)}
        />
      </FormField>
      <FormField label={meta.secretLabel} htmlFor="storage-new-secret" required>
        <Input
          id="storage-new-secret"
          type="password"
          className="font-mono"
          autoComplete="new-password"
          spellCheck={false}
          value={secretKey}
          disabled={busy}
          onChange={(event) => setSecretKey(event.target.value)}
        />
      </FormField>
      <Notice tone="info">
        保存前会先用新凭证测试连接，测试不通过则不会替换，正在使用的凭证不受影响。
      </Notice>
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onClose}>
          取消
        </Button>
        <Button type="submit" disabled={!ready || busy}>
          {busy && <Loader2 className="animate-spin" />}
          测试并替换
        </Button>
      </DialogFooter>
    </form>
  );
}
