import { Loader2, X } from "lucide-react";
import { AnimatePresence, MotionConfig, motion } from "motion/react";
import { useCallback, type KeyboardEvent } from "react";

import { getImportFile } from "@/api/admin/agent-skill";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/admin-ui/dialog";
import { Stepper, StepperItem } from "@/components/admin-ui/stepper";
import { Button } from "@/components/ui/button";
import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
import { confirmBlockReason } from "@/utils/admin/agent-skill";
import { stepOf } from "@/utils/admin/agent-skill-import";

import { FileWorkbench } from "./file-workbench";
import { ImportPick } from "./import-pick";
import { PrecheckBanner } from "./precheck-banner";
import type { useSkillImport } from "./use-skill-import";

/** 两步内容切换：旧内容上移淡出（exit 档），新内容从下方淡入（slow 档）；减少动态效果时由 MotionConfig 去掉位移 */
const STEP_MOTION = {
  initial: { opacity: 0, y: 8 },
  animate: { opacity: 1, y: 0, transition: { duration: DURATION.slow, ease: EASE_OUT } },
  exit: { opacity: 0, y: -4, transition: { duration: DURATION.exit, ease: EASE_OUT } },
};

/**
 * 导入对话框（居中大对话框，两步）：1 选择文件（含上传与预检进度）→ 2 检查并确认。
 * 两步尺寸不变，避免尺寸动画；窄屏全屏。⌘/Ctrl+↵ 在第 2 步确认；Esc 关闭（导入成本很低，不弹确认）。
 * 确认中不允许关闭。关闭由页面决定（页面在退出动画播完后才重置状态，内容不会提前闪回第 1 步）。
 * @param open 是否打开
 * @param importer 导入状态机（use-skill-import 的返回值）
 * @param dragActive 页面上正拖着文件，第 1 步拖放区高亮
 * @param onClose 请求关闭
 * @param onExited 退出动画播完
 */
export function ImportDialog({
  open,
  importer,
  dragActive,
  onClose,
  onExited,
}: {
  open: boolean;
  importer: ReturnType<typeof useSkillImport>;
  dragActive: boolean;
  onClose: () => void;
  onExited: () => void;
}) {
  const { state, begin, cancel, reselect, confirm } = importer;
  const step = stepOf(state);
  const confirming = state.step === "review" && state.confirming;
  const reason = state.step === "review" ? confirmBlockReason(state.view) : null;
  const viewId = state.step === "review" ? state.view.id : "";
  const readFile = useCallback((path: string) => getImportFile(viewId, path), [viewId]);

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      if (!reason) void confirm();
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && !confirming && onClose()}
      onOpenChangeComplete={(next) => !next && onExited()}
    >
      <DialogContent
        onKeyDown={onKeyDown}
        className="h-[min(720px,calc(100dvh-2rem))] w-[min(1080px,calc(100vw-2rem))] max-md:h-dvh max-md:w-screen max-md:rounded-none"
      >
        <MotionConfig reducedMotion="user">
          <DialogHeader>
            <DialogTitle>导入技能</DialogTitle>
            <DialogDescription className="sr-only">
              选择压缩包、文件夹或 SKILL.md，检查通过后确认导入。
            </DialogDescription>
            <Stepper className="mr-1 ml-auto max-w-sm">
              <StepperItem index={1} state={step === 1 ? "current" : "done"}>
                选择文件
              </StepperItem>
              <StepperItem index={2} state={step === 2 ? "current" : "todo"}>
                检查并确认
              </StepperItem>
            </Stepper>
            <DialogClose
              render={<Button variant="ghost" size="icon-sm" disabled={confirming} />}
              aria-label="关闭"
            >
              <X />
            </DialogClose>
          </DialogHeader>

          <AnimatePresence mode="wait" initial={false}>
            <motion.div key={step} {...STEP_MOTION} className="flex min-h-0 flex-1 flex-col">
              {state.step === "review" ? (
                <>
                  <PrecheckBanner view={state.view} />
                  <FileWorkbench
                    key={state.view.id}
                    className="mt-3.5 border-t"
                    files={state.view.files}
                    issues={state.view.issues}
                    readFile={readFile}
                    frontmatter={state.view.frontmatter}
                    unsupported={state.view.unsupported_fields}
                  />
                </>
              ) : (
                <ImportPick
                  state={state}
                  dragActive={dragActive}
                  onPick={(items) => void begin(items)}
                  onCancel={cancel}
                />
              )}
            </motion.div>
          </AnimatePresence>

          <DialogFooter>
            {state.step === "review" ? (
              <>
                <Button variant="outline" disabled={confirming} onClick={reselect}>
                  重新选择
                </Button>
                <div className="ml-auto flex items-center gap-2">
                  {reason && (
                    <span className="text-xs text-red-600 dark:text-red-400" role="status">
                      {reason}
                    </span>
                  )}
                  <Button variant="ghost" disabled={confirming} onClick={onClose}>
                    取消
                  </Button>
                  <motion.div whileTap={TAP} className="inline-flex">
                    <Button disabled={!!reason || confirming} onClick={() => void confirm()}>
                      {confirming && <Loader2 className="animate-spin" />}
                      确认导入
                      <kbd className="text-primary-foreground/70 ml-1 font-sans text-xs max-md:hidden">
                        ⌘↵
                      </kbd>
                    </Button>
                  </motion.div>
                </div>
              </>
            ) : (
              <>
                <p className="text-muted-foreground text-xs">导入后默认停用，不会影响线上 Agent</p>
                <Button variant="ghost" className="ml-auto" onClick={onClose}>
                  取消
                </Button>
              </>
            )}
          </DialogFooter>
        </MotionConfig>
      </DialogContent>
    </Dialog>
  );
}
