import { useEffect, useRef, useState } from "react";
import { Check } from "lucide-react";

import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  STAGE_INTERVAL_MS,
  TRANSFER_STAGES,
  stageIndexAt,
  stageStatus,
  type TransferKind,
} from "@/utils/canvas/transfer-stages";

/** 两个流程的标题和说明 */
const COPY: Record<TransferKind, { title: string; hint: string }> = {
  export: { title: "正在导出画布", hint: "导出期间请勿离开页面，也无法编辑画布" },
  import: { title: "正在导入画布", hint: "导入期间请勿离开页面" },
};

/**
 * 导入 / 导出进度弹窗（设计稿 docs/design/画布导入导出 第 14 节）：请求进行中把用户锁在这里。
 * 基于 AlertDialog：它本来就不响应点遮罩关闭，Esc 走 onOpenChange，受控的 open 不变就不会关。
 * 唯一能中止的方式是点「取消」，Esc、点遮罩、右上角都没有关闭入口，避免误触。
 * 取消会断开请求，后端随之停止复制并回滚事务；但事务提交那一瞬间之后停不住（设计稿 §14.8），
 * 所以点了取消后弹窗仍然锁住，等请求真正结束才由调用方关闭，避免取消后立刻又发起并发请求。
 * 被 Esc 或点遮罩拦住时，面板轻晃一次提示「现在不能关」。
 * 阶段列表是按时间轮播的说明，不代表后端真实进度，最后一步停住直到请求返回。
 * @param open 是否显示；请求开始置真，结束置假
 * @param kind 导出（画布 → 文件）或导入（文件 → 画布）
 * @param cancelling 已点取消、正在等请求收尾：按钮变「正在取消…」并禁用
 * @param onCancel 点「取消」
 */
export function TransferProgressDialog({
  open,
  kind,
  cancelling,
  onCancel,
}: {
  open: boolean;
  kind: TransferKind;
  cancelling: boolean;
  onCancel: () => void;
}) {
  const [nudging, setNudging] = useState(false);
  const nudgeTimer = useRef<ReturnType<typeof setTimeout>>(undefined);

  /** 关闭请求一律拒绝，只给个轻晃反馈 */
  const refuseClose = () => {
    setNudging(true);
    clearTimeout(nudgeTimer.current);
    nudgeTimer.current = setTimeout(() => setNudging(false), 120);
  };
  useEffect(() => () => clearTimeout(nudgeTimer.current), []);

  // 请求进行中关页面 / 刷新会让请求半途而废，交给浏览器自带的离开确认
  useEffect(() => {
    if (!open) return;
    const guard = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [open]);

  return (
    <AlertDialog open={open} onOpenChange={(next) => !next && refuseClose()}>
      <AlertDialogContent
        className={cn(
          "w-[min(calc(100%-2rem),22rem)] items-stretch gap-0 p-5 text-center",
          nudging && "transfer-nudge",
        )}
      >
        <TransferStage kind={kind} />
        <AlertDialogTitle className="mt-4 text-[15px] font-semibold">
          {COPY[kind].title}
        </AlertDialogTitle>
        <AlertDialogDescription className="mt-0.5 text-[13px]">
          {COPY[kind].hint}
        </AlertDialogDescription>
        <StageList kind={kind} />
        <Button
          type="button"
          variant="outline"
          className="mx-auto mt-4 min-w-24"
          disabled={cancelling}
          onClick={onCancel}
        >
          {cancelling ? "正在取消…" : "取消"}
        </Button>
      </AlertDialogContent>
    </AlertDialog>
  );
}

/** 动画区：文件和画布之间，包裹沿轨道飞过去；导出时左右镜像，方向反过来 */
function TransferStage({ kind }: { kind: TransferKind }) {
  return (
    <div
      aria-hidden
      className={cn("relative mx-auto h-16 w-[132px]", kind === "export" && "-scale-x-100")}
    >
      <div
        className={cn(
          "border-foreground bg-popover absolute top-2 left-0 h-12 w-10 rounded-lg border-[1.5px]",
          kind === "export" && "-scale-x-100",
        )}
      >
        {/* 文件内容的三行线条 */}
        <div className="bg-foreground/35 absolute inset-x-[7px] top-3 h-0.5 rounded-full shadow-[0_8px_0_var(--color-foreground),0_16px_0_var(--color-foreground)]" />
      </div>
      <div className="bg-border absolute top-[31px] right-[46px] left-[46px] h-0.5 rounded-full" />
      {[0, 0.46, 0.92].map((delay) => (
        <i
          key={delay}
          className="transfer-packet bg-status-running absolute top-[25px] left-[46px] size-3.5 rounded"
          style={{ animationDelay: `${delay}s` }}
        />
      ))}
      <div
        className={cn(
          "border-foreground bg-popover absolute top-2 right-0 h-12 w-10 rounded-lg border-[1.5px]",
          kind === "export" && "-scale-x-100",
        )}
      >
        <div className="border-status-running transfer-breathe absolute inset-x-2 inset-y-[9px] rounded border-[1.5px]" />
      </div>
    </div>
  );
}

/** 阶段列表：按时间推进，已完成打勾、进行中转圈、未开始空心圆 */
function StageList({ kind }: { kind: TransferKind }) {
  const stages = TRANSFER_STAGES[kind];
  const [current, setCurrent] = useState(0);

  // AlertDialog 关闭后内容会卸载，下次打开重新挂载，所以每次都是从第一步开始；到最后一步就不再前进
  useEffect(() => {
    const startedAt = Date.now();
    const timer = setInterval(() => {
      setCurrent(stageIndexAt(Date.now() - startedAt, stages.length));
    }, STAGE_INTERVAL_MS / 2);
    return () => clearInterval(timer);
  }, [stages.length]);

  return (
    <ul role="status" aria-live="polite" className="mt-4 grid gap-1.5 text-left text-[13px]">
      {stages.map((label, index) => {
        const status = stageStatus(index, current);
        return (
          <li
            key={label}
            className={cn(
              "flex items-center gap-2 transition-colors duration-[180ms]",
              status === "todo" ? "text-muted-foreground" : "text-foreground",
            )}
          >
            <StageDot status={status} />
            {label}
          </li>
        );
      })}
    </ul>
  );
}

/** 阶段前的小圆点：done 实心打勾，now 转圈，todo 空心 */
function StageDot({ status }: { status: ReturnType<typeof stageStatus> }) {
  if (status === "done") {
    return (
      <span className="bg-status-running text-primary-foreground grid size-3.5 flex-none place-items-center rounded-full">
        <Check className="size-2.5" strokeWidth={3} />
      </span>
    );
  }
  return (
    <span
      className={cn(
        "size-3.5 flex-none rounded-full border-[1.5px]",
        status === "now"
          ? "border-status-running border-t-transparent motion-safe:animate-spin motion-reduce:border-t-status-running"
          : "border-current opacity-45",
      )}
    />
  );
}
