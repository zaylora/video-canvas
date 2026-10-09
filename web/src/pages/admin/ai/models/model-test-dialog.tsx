import { FlaskConical } from "lucide-react";

import { VendorAvatar } from "@/components/admin-ui/vendor-avatar";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@/components/ui/dialog";
import { readModelKind, readModelPricing, readModelString } from "@/utils/admin/model-body";

import { ResultPanel } from "../result-panel";
import { TestNode } from "./test-node";
import type { ModelWorkspace } from "./use-model-workspace";

/**
 * 测试模型（设计稿的二级弹窗）：左边是模拟画布节点，像在画布上一样选参数、传素材；
 * 右边是结果 / 请求描述 / 追踪 / 日志。用当前配置真实调用一次上游，不扣用户积分，结果不进素材库。
 * 有未保存的修改会先自动保存。
 */
export function ModelTestDialog({
  open,
  ws,
  onClose,
  onClosed,
}: {
  open: boolean;
  ws: ModelWorkspace;
  onClose: () => void;
  /** 退出动画播完后回调 */
  onClosed?: () => void;
}) {
  const body = ws.body;
  const label = body ? readModelString(body, "label") : "";
  const kind = body ? readModelKind(body) : "";
  const busy = ws.busy === "test-run" || ws.busy === "dry-run" || ws.busy === "save";

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && onClose()}
      onOpenChangeComplete={(next) => {
        if (!next) onClosed?.();
      }}
    >
      <DialogContent className="flex h-[min(88svh,820px)] flex-col gap-0 overflow-hidden p-0 sm:max-w-5xl">
        <div className="flex items-start gap-3 border-b px-6 py-4 pr-12">
          <VendorAvatar
            vendor={body ? readModelString(body, "vendor") : ""}
            name={label || "?"}
            seed={ws.modelKey}
          />
          <div className="min-w-0 flex-1">
            <DialogTitle className="text-base font-semibold">测试 {label || "模型"}</DialogTitle>
            <DialogDescription className="text-xs">
              用当前配置真实调用一次上游，不扣用户积分，结果不进素材库。
              {ws.dirty && "有未保存的修改，开始前会先保存。"}
            </DialogDescription>
          </div>
        </div>

        <div className="grid min-h-0 flex-1 lg:grid-cols-[22rem_minmax(0,1fr)]">
          <div className="bg-muted/20 min-h-0 overflow-y-auto border-b p-5 lg:border-r lg:border-b-0">
            <TestNode
              modelKey={ws.modelKey}
              vendor={body ? readModelString(body, "vendor") : ""}
              label={label}
              kind={kind}
              pricing={body ? (readModelPricing(body) ?? undefined) : undefined}
              caps={ws.capabilities}
              params={ws.testParams}
              assets={ws.testAssets}
              errors={ws.testInput.errors}
              showErrors={ws.showTestErrors}
              disabled={busy}
              onChange={ws.setTestParam}
              onAddRef={ws.addTestRef}
              onRemoveRef={ws.removeTestRef}
              action={{
                label: "开始测试",
                busy: ws.busy === "test-run",
                disabled: busy,
                onClick: () => void ws.doTestRun(),
              }}
            />
            <Button
              variant="outline"
              size="sm"
              className="mt-3 w-full"
              disabled={busy}
              onClick={() => void ws.doDryRun()}
            >
              <FlaskConical />
              只看请求（不发送）
            </Button>
          </div>
          <div className="flex min-h-0 flex-col">
            <ResultPanel
              tab={ws.resultTab}
              onTabChange={ws.setResultTab}
              modelKey={ws.modelKey}
              dryRun={ws.dryRun}
              run={ws.run}
              trace={ws.trace}
              onRefreshTrace={ws.refreshTrace}
            />
          </div>
        </div>

        <DialogFooter className="mx-0 mb-0 rounded-none px-6 py-3.5">
          <Button variant="outline" onClick={onClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
