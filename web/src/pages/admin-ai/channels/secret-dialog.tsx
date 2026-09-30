import { useState } from "react";
import { Loader2 } from "lucide-react";

import { setChannelSecret } from "@/api/admin-ai";
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

import { FormField, Notice } from "../shared";
import { useAliveRef } from "../use-admin";

/**
 * 设置 / 更新渠道 Key（仅运维）。Key 永远只写：
 * - 输入框永远从空白开始，type=password、autocomplete=new-password；
 * - 明文只存在于这个对话框的 state 里，提交时取走并立刻清空，不进 URL、store、localStorage、日志；
 * - 已设置时先在对话框内二次确认“将覆盖现有 Key”，第二次点击才真正提交。
 * @param secretSet 渠道当前是否已设置 Key
 * @param onSaved 保存成功后（刷新渠道的“已设置”状态）
 */
export function SecretDialog({
  open,
  channelKey,
  channelName,
  secretSet,
  onClose,
  onSaved,
}: {
  open: boolean;
  channelKey: string;
  channelName: string;
  secretSet: boolean;
  onClose: () => void;
  onSaved: () => void;
}) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-md">
        {/* 每次打开都重新挂载：输入框永远是空的，上一次的输入不会残留 */}
        {open && (
          <SecretBody
            channelKey={channelKey}
            channelName={channelName}
            secretSet={secretSet}
            onClose={onClose}
            onSaved={onSaved}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function SecretBody({
  channelKey,
  channelName,
  secretSet,
  onClose,
  onSaved,
}: {
  channelKey: string;
  channelName: string;
  secretSet: boolean;
  onClose: () => void;
  onSaved: () => void;
}) {
  const aliveRef = useAliveRef();
  const [value, setValue] = useState("");
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    if (!value.trim() || busy) return;
    if (secretSet && !confirming) {
      setConfirming(true);
      return;
    }
    // 取走明文并立刻清空输入：请求期间与之后 state 里都不再持有 Key
    const plain = value;
    setValue("");
    setBusy(true);
    try {
      await setChannelSecret(channelKey, plain);
      if (!aliveRef.current) return;
      onSaved();
      onClose();
    } catch {
      // 全局 toast 已弹；回到未确认状态，让用户重新输入（输入已清空，不回显）
      if (aliveRef.current) setConfirming(false);
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
        <DialogTitle>{secretSet ? "更新 Key" : "设置 Key"}</DialogTitle>
        <DialogDescription>
          渠道 <b>{channelName}</b>（<span className="font-mono">{channelKey}</span>）。Key 只写不读，保存后不会再显示。
        </DialogDescription>
      </DialogHeader>

      <FormField label="Key" htmlFor="channel-secret-input">
        <Input
          id="channel-secret-input"
          type="password"
          autoComplete="new-password"
          spellCheck={false}
          placeholder="粘贴 Key"
          value={value}
          disabled={busy}
          onChange={(event) => {
            setValue(event.target.value);
            setConfirming(false);
          }}
        />
      </FormField>

      {secretSet && (
        <Notice tone={confirming ? "danger" : "warning"} title={confirming ? "再点一次确认覆盖" : undefined}>
          这将<b>覆盖现有 Key</b>，正在使用该 Key 的任务会立即改用新 Key。
        </Notice>
      )}

      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onClose}>
          取消
        </Button>
        <Button
          type="submit"
          variant={confirming ? "destructive" : "default"}
          disabled={!value.trim() || busy}
        >
          {busy && <Loader2 className="animate-spin" />}
          {confirming ? "确认覆盖" : "保存"}
        </Button>
      </DialogFooter>
    </form>
  );
}
