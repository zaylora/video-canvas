import { useState } from "react";
import { Loader2 } from "lucide-react";

import { setChannelSecret, setChannelSecretKey } from "@/api/admin/ai";
import { FormField } from "@/components/admin-ui/form-field";
import { Notice } from "@/components/admin-ui/notice";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { openDialog, type DialogControl } from "@/store/dialog";

import { useAliveRef } from "../../use-admin";

/** 设置 Key 弹窗的业务参数 */
export type SecretDialogProps = {
  /** 渠道 key */
  channelKey: string;
  /** 渠道显示名 */
  channelName: string;
  /** API Key 是否已设置 */
  secretSet: boolean;
  /** Secret Key 是否已设置；已设置时才给“清除”勾选 */
  secretKeySet?: boolean;
  /** 保存成功后（刷新渠道清单的“已设置”状态）；弹窗会自己关闭 */
  onSaved: () => void;
};

/**
 * 设置渠道 Key（仅 super_admin）：API Key 与 Secret Key（可选），都留空表示不修改。Key 永远只写：
 * - 输入框永远从空白开始，type=password、autocomplete=new-password；
 * - 明文只存在于这个对话框的 state 里，提交时取走并立刻清空，不进 URL、store、localStorage、日志；
 * - Secret Key 已设置时可勾选“清除”（提交空串）。
 * 走全局弹窗 store（openSecretDialog）。
 */
export function SecretDialog({
  open,
  onClose,
  onExited,
  ...props
}: SecretDialogProps & DialogControl) {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && onClose()}
      onOpenChangeComplete={(next) => !next && onExited()}
    >
      <DialogContent className="sm:max-w-md">
        {/* 弹层收起后 Base UI 会卸载内容，下次打开重新挂载，输入框仍然从空白开始 */}
        <SecretBody {...props} onClose={onClose} />
      </DialogContent>
    </Dialog>
  );
}

function SecretBody({
  channelKey,
  channelName,
  secretSet,
  secretKeySet = false,
  onClose,
  onSaved,
}: SecretDialogProps & { onClose: () => void }) {
  const aliveRef = useAliveRef();
  const [apiKey, setApiKey] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [clearSecret, setClearSecret] = useState(false);
  const [busy, setBusy] = useState(false);
  const [partial, setPartial] = useState<string | null>(null);

  const hasApi = apiKey.trim() !== "";
  const hasSecret = secretKey.trim() !== "";
  const willClear = secretKeySet && clearSecret && !hasSecret;
  const canSubmit = (hasApi || hasSecret || willClear) && !busy;

  const submit = async () => {
    if (!canSubmit) return;
    // 取走明文并立刻清空输入：请求期间与之后 state 里都不再持有 Key
    const plainApi = apiKey.trim();
    const plainSecret = secretKey.trim();
    setApiKey("");
    setSecretKey("");
    setPartial(null);
    setBusy(true);
    let apiSaved = false;
    try {
      if (plainApi) {
        await setChannelSecret(channelKey, plainApi);
        apiSaved = true;
      }
      if (plainSecret) await setChannelSecretKey(channelKey, plainSecret);
      else if (willClear) await setChannelSecretKey(channelKey, "");
      if (!aliveRef.current) return;
      onSaved();
      onClose();
    } catch {
      // 全局 toast 已弹；API Key 已写入而 Secret Key 失败时，要让用户知道哪一半成功了
      if (!aliveRef.current) return;
      if (apiSaved) {
        onSaved();
        setPartial("API Key 已保存，但 Secret Key 没有保存成功，请重新输入 Secret Key 再保存。");
      }
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
        <DialogTitle>设置「{channelName}」的 Key</DialogTitle>
        <DialogDescription>
          渠道 key：<span className="font-mono">{channelKey}</span>
        </DialogDescription>
      </DialogHeader>

      <FormField label="API Key" htmlFor="channel-api-key">
        <Input
          id="channel-api-key"
          type="password"
          autoComplete="new-password"
          spellCheck={false}
          placeholder={secretSet ? "已设置，留空表示不修改" : "粘贴 API Key"}
          value={apiKey}
          disabled={busy}
          onChange={(event) => setApiKey(event.target.value)}
        />
      </FormField>

      <FormField
        label="Secret Key（可选）"
        htmlFor="channel-secret-key"
        hint="仅 AK/SK 签名类协议需要（如可灵）；其他渠道留空。"
      >
        <Input
          id="channel-secret-key"
          type="password"
          autoComplete="new-password"
          spellCheck={false}
          placeholder={secretKeySet ? "已设置，留空表示不修改" : "粘贴 Secret Key"}
          value={secretKey}
          disabled={busy}
          onChange={(event) => setSecretKey(event.target.value)}
        />
        {secretKeySet && (
          <div className="mt-1 flex items-center gap-2 text-xs">
            <Checkbox
              id="channel-secret-key-clear"
              checked={clearSecret}
              disabled={busy || hasSecret}
              onCheckedChange={(checked) => setClearSecret(checked === true)}
            />
            <Label htmlFor="channel-secret-key-clear" className="text-xs font-normal">
              清除已设置的 Secret Key
            </Label>
          </div>
        )}
      </FormField>

      {secretSet && hasApi && (
        <Notice tone="warning">
          将<b>覆盖现有 API Key</b>，正在使用它的任务会立即改用新 Key。
        </Notice>
      )}
      {partial && <Notice tone="danger">{partial}</Notice>}

      <p className="text-muted-foreground text-xs">
        Key 只写不读，保存后不会再显示明文；设置会写审计（不含值）。
      </p>

      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onClose}>
          取消
        </Button>
        <Button type="submit" disabled={!canSubmit}>
          {busy && <Loader2 className="animate-spin" />}
          保存
        </Button>
      </DialogFooter>
    </form>
  );
}

/**
 * 用全局弹窗 store 打开设置 Key 弹窗（渠道页、总览页上直接打开时用）。
 * @param props 渠道信息与保存后的回调
 */
export const openSecretDialog = (props: SecretDialogProps) => openDialog(SecretDialog, props);
