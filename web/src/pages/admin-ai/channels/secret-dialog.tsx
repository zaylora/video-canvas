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

import { FormField } from "@/components/admin-ui/form-field";
import { Notice } from "@/components/admin-ui/notice";
import { openDialog } from "@/store/dialog";
import { useAliveRef } from "../use-admin";

/**
 * 设置 / 更新渠道 Key（仅运维）。Key 永远只写：
 * - 输入框永远从空白开始，type=password、autocomplete=new-password；
 * - 明文只存在于这个对话框的 state 里，提交时取走并立刻清空，不进 URL、store、localStorage、日志；
 * - 已设置时先在对话框内二次确认“将覆盖现有 Key”，第二次点击才真正提交。
 * @param secretSet 渠道当前是否已设置 Key
 * 页面上直接打开时走全局弹窗 store（openDialog(SecretDialog, …)）；在渠道抽屉里是受控的子弹窗。
 * @param onSaved 保存成功后（刷新渠道的“已设置”状态）
 * @param onExited 退出动画播完（store 管理时传入）
 */
export function SecretDialog({
  open,
  channelKey,
  channelName,
  secretSet,
  onClose,
  onSaved,
  onExited,
}: {
  open: boolean;
  channelKey: string;
  channelName: string;
  secretSet: boolean;
  onClose: () => void;
  onSaved: () => void;
  onExited?: () => void;
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && onClose()}
      onOpenChangeComplete={(next) => !next && onExited?.()}
    >
      <DialogContent className="sm:max-w-md">
        {/* 不用 open && 包：关闭时内容要留到退出动画播完。弹层收起后 Base UI 会卸载它，
            下次打开重新挂载，输入框仍然从空白开始，上一次的输入不会残留 */}
        <SecretBody
          channelKey={channelKey}
          channelName={channelName}
          secretSet={secretSet}
          onClose={onClose}
          onSaved={onSaved}
        />
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
          渠道 <b>{channelName}</b>（<span className="font-mono">{channelKey}</span>）。Key
          只写不读，保存后不会再显示。
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
        <Notice
          tone={confirming ? "danger" : "warning"}
          title={confirming ? "再点一次确认覆盖" : undefined}
        >
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

/**
 * 用全局弹窗 store 打开 Key 弹窗（渠道页、总览页上直接打开时用）。
 * @param onSaved 保存成功后（刷新渠道清单）；弹窗会自己关闭
 */
export const openSecretDialog = (props: {
  channelKey: string;
  channelName: string;
  secretSet: boolean;
  onSaved: () => void;
}) => openDialog(SecretDialog, props);
