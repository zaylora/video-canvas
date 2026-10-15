import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

import {
  createChannel,
  setChannelSecret,
  setChannelSecretKey,
  updateChannel,
} from "@/api/admin/ai";
import type { ChannelView } from "@/api/admin/ai/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
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
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { openDialog, type DialogControl } from "@/store/dialog";
import {
  buildChannelRequest,
  channelFormFromView,
  emptyChannelForm,
  type ChannelFormState,
} from "@/utils/admin/channel-form";
import { ADMIN_ERROR_CODE, errorMessage } from "@/utils/admin/errors";

import { useAliveRef } from "../../use-admin";
import { HeaderEditor } from "./header-editor";

/** 新增 / 编辑渠道弹窗的业务参数 */
export type ChannelDialogProps = {
  /** 编辑的渠道；不传表示新增 */
  channel?: ChannelView | null;
  /** 保存成功后（新建或更新；刷新渠道清单与选中项）；参数是渠道 key */
  onSaved: (key: string, created: boolean) => void;
};

/** 业务校验失败（50013）：原因在 message 里，展示在弹窗顶部 */
const isChannelInvalid = (error: unknown) =>
  typeof error === "object" &&
  error !== null &&
  (error as { code?: unknown }).code === ADMIN_ERROR_CODE.CHANNEL_INVALID;

/**
 * 新增 / 编辑渠道弹窗（仅 super_admin），走全局弹窗 store（openChannelDialog）。
 * 渠道只有连接信息：名称、key、Base URL、API Key / Secret Key（仅新建时填，之后用“设置 Key”）、自定义请求头、限流、启用；
 * 协议在模型里选。保存只做字段校验，不向上游发请求。
 * 有未保存的修改时关闭先二次确认。
 */
export function ChannelDialog({
  open,
  onClose,
  onExited,
  ...props
}: ChannelDialogProps & DialogControl) {
  return (
    <ChannelDialogShell open={open} onClose={onClose} onExited={onExited}>
      {(requestClose, setDirty) => (
        <ChannelForm
          {...props}
          onClose={onClose}
          onRequestClose={requestClose}
          onDirty={setDirty}
        />
      )}
    </ChannelDialogShell>
  );
}

/** 弹窗外壳：把“有没有改动”交给内部表单上报，关闭时有改动先确认 */
function ChannelDialogShell({
  open,
  onClose,
  onExited,
  children,
}: DialogControl & {
  children: (requestClose: () => void, setDirty: (dirty: boolean) => void) => ReactNode;
}) {
  const [dirty, setDirty] = useState(false);
  const requestClose = () => {
    if (!dirty) return onClose();
    void confirm({
      title: "放弃未保存的修改？",
      description: "关闭后，这次对渠道的修改不会保存。",
      confirmLabel: "放弃修改",
      destructive: true,
    }).then((ok) => ok && onClose());
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && requestClose()}
      onOpenChangeComplete={(next) => !next && onExited()}
    >
      <DialogContent
        showCloseButton={false}
        className="flex max-h-[min(90svh,860px)] flex-col gap-0 overflow-hidden p-0 sm:max-w-xl"
      >
        {children(requestClose, setDirty)}
      </DialogContent>
    </Dialog>
  );
}

function ChannelForm({
  channel,
  onSaved,
  onClose,
  onRequestClose,
  onDirty,
}: ChannelDialogProps & {
  onClose: () => void;
  onRequestClose: () => void;
  onDirty: (dirty: boolean) => void;
}) {
  const aliveRef = useAliveRef();
  const isNew = !channel;
  const [initial] = useState(() => (channel ? channelFormFromView(channel) : emptyChannelForm()));
  const [form, setForm] = useState<ChannelFormState>(initial);
  const [apiKey, setApiKey] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [attempted, setAttempted] = useState(false);
  const [busy, setBusy] = useState(false);
  /** 后端业务校验失败的原因（50013），展示在弹窗顶部 */
  const [topError, setTopError] = useState<string | null>(null);

  const dirty =
    JSON.stringify(form) !== JSON.stringify(initial) || apiKey !== "" || secretKey !== "";
  useEffect(() => onDirty(dirty), [dirty, onDirty]);

  const built = useMemo(() => buildChannelRequest(form, channel), [form, channel]);
  const errors = attempted && !built.ok ? built.errors : {};

  const patch = (next: Partial<ChannelFormState>) => {
    setForm((prev) => {
      return { ...prev, ...next };
    });
  };

  const submit = async () => {
    if (busy) return;
    setAttempted(true);
    setTopError(null);
    if (!built.ok) return;
    setBusy(true);
    try {
      if (!channel) {
        // 渠道 key 是内部标识，由后端生成并在响应里返回
        const created = await createChannel(built.create);
        // 渠道已经建好：之后任何一步失败都不能再留在弹窗里重复创建，改为提示去“设置 Key”
        const key = created.key;
        const plainApi = apiKey.trim();
        const plainSecret = secretKey.trim();
        setApiKey("");
        setSecretKey("");
        const failed: string[] = [];
        if (plainApi) {
          try {
            await setChannelSecret(key, plainApi);
          } catch {
            failed.push("API Key");
          }
        }
        if (plainSecret) {
          try {
            await setChannelSecretKey(key, plainSecret);
          } catch {
            failed.push("Secret Key");
          }
        }
        if (failed.length > 0) {
          toast.warning(`渠道已创建，但 ${failed.join("、")} 没有设置成功`, {
            description: "请在渠道详情里点击“设置 Key”重新设置。",
          });
        } else {
          toast.success("渠道已创建。下一步：拉取模型");
        }
        onSaved(key, true);
        onClose();
        return;
      }
      if (built.changed) {
        await updateChannel(channel.key, built.update);
        toast.success("渠道已更新");
        onSaved(channel.key, false);
      }
      onClose();
    } catch (cause) {
      // 全局 toast 已弹；这里把原因留在弹窗里，字段级问题挂到字段上
      if (!aliveRef.current) return;
      if (isChannelInvalid(cause)) setTopError(errorMessage(cause, "渠道配置不合法"));
    } finally {
      if (aliveRef.current) setBusy(false);
    }
  };

  return (
    <form
      className="flex min-h-0 flex-1 flex-col"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <DialogHeader className="border-b p-4">
        <DialogTitle>{isNew ? "新增渠道" : "编辑渠道"}</DialogTitle>
        <DialogDescription>
          渠道只管连接信息：地址、Key、请求头、限流。请求协议在模型里选。
        </DialogDescription>
      </DialogHeader>

      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-4">
        {topError && <Notice tone="danger">{topError}</Notice>}

        <FormField label="渠道名称" htmlFor="channel-name" required error={errors.name}>
          <Input
            id="channel-name"
            placeholder="例如：OpenAI 官方渠道"
            autoComplete="off"
            value={form.name}
            disabled={busy}
            aria-invalid={!!errors.name}
            onChange={(event) => patch({ name: event.target.value })}
          />
        </FormField>

        <FormField
          label="Base URL"
          htmlFor="channel-base-url"
          required
          error={errors.baseUrl}
          hint="填写上游 Base URL，例如 https://api.example.com；不要带查询参数。"
        >
          <Input
            id="channel-base-url"
            placeholder="https://api.example.com"
            autoComplete="off"
            spellCheck={false}
            value={form.baseUrl}
            disabled={busy}
            aria-invalid={!!errors.baseUrl}
            onChange={(event) => patch({ baseUrl: event.target.value })}
          />
        </FormField>

        {isNew ? (
          <>
            <FormField label="API Key" htmlFor="channel-new-api-key">
              <Input
                id="channel-new-api-key"
                type="password"
                autoComplete="new-password"
                spellCheck={false}
                placeholder="API Key"
                value={apiKey}
                disabled={busy}
                onChange={(event) => setApiKey(event.target.value)}
              />
            </FormField>
            <FormField
              label="Secret Key（可选）"
              htmlFor="channel-new-secret-key"
              hint="仅 AK/SK 签名类协议需要（如可灵）；其他渠道留空。"
            >
              <Input
                id="channel-new-secret-key"
                type="password"
                autoComplete="new-password"
                spellCheck={false}
                placeholder="Secret Key"
                value={secretKey}
                disabled={busy}
                onChange={(event) => setSecretKey(event.target.value)}
              />
            </FormField>
          </>
        ) : (
          <p className="text-muted-foreground text-xs">
            API Key 与 Secret Key 只写不读，请用“设置 Key”修改。
          </p>
        )}

        <HeaderEditor
          rows={form.headers}
          errors={errors}
          disabled={busy}
          onChange={(headers) => patch({ headers })}
        />

        <div className="flex flex-col gap-1.5">
          <Label className="text-muted-foreground text-xs">限流（可选，留空不限）</Label>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <FormField label="每秒请求数 rps" htmlFor="channel-rps" error={errors.rps}>
              <Input
                id="channel-rps"
                type="number"
                min={0}
                value={form.rps}
                disabled={busy}
                aria-invalid={!!errors.rps}
                onChange={(event) => patch({ rps: event.target.value })}
              />
            </FormField>
            <FormField
              label="最大同时请求数"
              htmlFor="channel-concurrency"
              error={errors.maxConcurrency}
            >
              <Input
                id="channel-concurrency"
                type="number"
                min={0}
                value={form.maxConcurrency}
                disabled={busy}
                aria-invalid={!!errors.maxConcurrency}
                onChange={(event) => patch({ maxConcurrency: event.target.value })}
              />
            </FormField>
            <FormField
              label="最大同时生成数"
              htmlFor="channel-max-running"
              error={errors.maxRunning}
            >
              <Input
                id="channel-max-running"
                type="number"
                min={0}
                value={form.maxRunning}
                disabled={busy}
                aria-invalid={!!errors.maxRunning}
                onChange={(event) => patch({ maxRunning: event.target.value })}
              />
            </FormField>
          </div>
        </div>

        <div className="flex items-center gap-2 text-sm">
          <Switch
            id="channel-enabled"
            checked={form.enabled}
            disabled={busy}
            onCheckedChange={(checked) => patch({ enabled: checked })}
          />
          <Label htmlFor="channel-enabled">启用</Label>
          <span className="text-muted-foreground text-xs">
            停用后它的模型在画布里不可见，进行中的任务按快照继续
          </span>
        </div>
      </div>

      <DialogFooter className="m-0 rounded-none">
        <Button type="button" variant="outline" disabled={busy} onClick={onRequestClose}>
          取消
        </Button>
        <Button type="submit" disabled={busy}>
          {busy && <Loader2 className="animate-spin" />}
          保存
        </Button>
      </DialogFooter>
    </form>
  );
}

/**
 * 用全局弹窗 store 打开新增 / 编辑渠道弹窗。
 * @param props 编辑的渠道（不传为新增）、已有渠道 key、保存后的回调
 */
export const openChannelDialog = (props: ChannelDialogProps) => openDialog(ChannelDialog, props);
