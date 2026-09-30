import { useRef, useState, type DragEvent } from "react";
import { FileCode2, Loader2 } from "lucide-react";

import { uploadPlugin } from "@/api/admin-ai";
import type { PluginUploadResult } from "@/api/admin-ai/type";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { isRunnerDown, isTooLarge } from "@/utils/admin/errors";
import { checkPluginFile, shortSha, summarizeUpload, PLUGIN_MAX_BYTES } from "@/utils/admin/plugin";

import { CopyButton, Notice } from "../shared";
import { useAliveRef } from "../use-admin";

/**
 * 上传插件对话框（仅运维）。
 * - 选文件后先做本地检查（扩展名、大小），不通过不发请求；
 * - 预检不通过也是 HTTP 200：问题精确到字段路径、内联显示，对话框不关闭，用户换文件可再传；
 * - 通过后切成功态，给“下一步：新建渠道”；
 * - 503 / 50021：提示 runner 不可用，保留已选文件可重试。
 * @param onUploaded 预检通过并登记后（刷新插件清单）
 * @param onNextStep 点“下一步：新建渠道”，带上插件 key
 */
export function UploadDialog({
  open,
  onClose,
  onUploaded,
  onNextStep,
}: {
  open: boolean;
  onClose: () => void;
  onUploaded: () => void;
  onNextStep: (pluginKey: string) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-xl">
        {/* 每次打开都重新挂载，清掉上一次的文件与结果 */}
        {open && <UploadBody onClose={onClose} onUploaded={onUploaded} onNextStep={onNextStep} />}
      </DialogContent>
    </Dialog>
  );
}

function UploadBody({
  onClose,
  onUploaded,
  onNextStep,
}: {
  onClose: () => void;
  onUploaded: () => void;
  onNextStep: (pluginKey: string) => void;
}) {
  const aliveRef = useAliveRef();
  const inputRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [localError, setLocalError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<PluginUploadResult | null>(null);
  const [runnerDown, setRunnerDown] = useState(false);
  const [dragging, setDragging] = useState(false);

  const summary = result ? summarizeUpload(result) : null;
  const accepted = !!result?.accepted;

  const pick = (next: File | null) => {
    setResult(null);
    setRunnerDown(false);
    if (!next) {
      setFile(null);
      setLocalError(null);
      return;
    }
    setFile(next);
    setLocalError(checkPluginFile(next));
  };

  const onDrop = (event: DragEvent<HTMLLabelElement>) => {
    event.preventDefault();
    setDragging(false);
    pick(event.dataTransfer.files?.[0] ?? null);
  };

  const submit = async () => {
    if (!file || localError || busy) return;
    setBusy(true);
    setRunnerDown(false);
    setResult(null);
    try {
      const next = await uploadPlugin(file);
      if (!aliveRef.current) return;
      setResult(next);
      if (next.accepted) onUploaded();
    } catch (error) {
      // 全局 toast 已弹；这里只把可恢复的两类原因翻译成对话框内的就地提示
      if (!aliveRef.current) return;
      if (isRunnerDown(error)) setRunnerDown(true);
      else if (isTooLarge(error)) setLocalError(`文件超过 ${PLUGIN_MAX_BYTES / 1024}KB 上限`);
    } finally {
      if (aliveRef.current) setBusy(false);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>上传插件</DialogTitle>
        <DialogDescription>
          选择一个 .js 插件文件（≤512KB）。上传后会先做预检，通过才登记为新版本；版本一经登记不可修改。
        </DialogDescription>
      </DialogHeader>

      {!accepted && (
        <label
          className={cn(
            "flex cursor-pointer flex-col items-center gap-1 rounded-lg border border-dashed p-4 text-center text-sm transition-colors",
            dragging && "border-primary bg-primary/5",
            file && "border-solid",
          )}
          onDragOver={(event) => {
            event.preventDefault();
            setDragging(true);
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={onDrop}
        >
          <FileCode2 className="text-muted-foreground size-5" />
          {file ? (
            <span>
              已选：<span className="font-mono">{file.name}</span> · {(file.size / 1024).toFixed(1)}KB
            </span>
          ) : (
            <span className="text-muted-foreground">拖拽或点击选择 .js 文件</span>
          )}
          <input
            ref={inputRef}
            type="file"
            accept=".js,text/javascript"
            className="sr-only"
            aria-label="选择插件文件"
            disabled={busy}
            onChange={(event) => {
              pick(event.target.files?.[0] ?? null);
              // 允许连续两次选同一个文件
              event.target.value = "";
            }}
          />
        </label>
      )}

      {localError && (
        <Notice tone="danger">
          {localError}（本地已拦截，未发请求）
        </Notice>
      )}

      {busy && (
        <p className="text-muted-foreground flex items-center gap-1.5 text-xs" role="status">
          <Loader2 className="size-3 animate-spin" />
          预检中…（在沙箱里编译并检查钩子）
        </p>
      )}

      {runnerDown && (
        <Notice tone="danger" title="插件运行器暂时不可用">
          已选文件保留，稍后点“上传”重试。
        </Notice>
      )}

      {summary && !accepted && (
        <section aria-label="预检结果" className="flex flex-col gap-2">
          <Notice tone="danger" title={summary.title} />
          <ul className="flex max-h-64 flex-col gap-1.5 overflow-y-auto">
            {summary.issues.map((issue, index) => (
              <li key={`${issue.path}-${index}`} className="bg-destructive/5 flex items-start gap-2 rounded-md px-2.5 py-2">
                <div className="min-w-0 flex-1">
                  <code className="text-destructive text-xs font-semibold break-all">
                    {issue.path || "（整个文件）"}
                  </code>
                  <p className="text-xs break-words">{issue.message}</p>
                </div>
                {issue.path && <CopyButton iconOnly text={issue.path} label="复制路径" />}
              </li>
            ))}
          </ul>
          <p className="text-muted-foreground text-xs">修改文件后重新选择即可再次上传。</p>
        </section>
      )}

      {accepted && result?.version && (
        <Notice tone="success" title={summary?.title}>
          <dl className="mt-1 grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5">
            <dt className="opacity-70">版本</dt>
            <dd className="font-mono">{result.version.version}</dd>
            <dt className="opacity-70">sha256</dt>
            <dd className="font-mono" title={result.version.sha256}>
              {shortSha(result.version.sha256)}…
            </dd>
          </dl>
        </Notice>
      )}

      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          {accepted ? "完成" : "取消"}
        </Button>
        {accepted && result?.version ? (
          <Button onClick={() => onNextStep(result.version!.plugin_key)}>下一步：新建渠道</Button>
        ) : (
          <Button disabled={!file || !!localError || busy} onClick={() => void submit()}>
            {busy && <Loader2 className="animate-spin" />}
            {busy ? "预检中…" : "上传"}
          </Button>
        )}
      </DialogFooter>
    </>
  );
}
