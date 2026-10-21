import { CopyButton } from "@/components/admin-ui/copy-button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/admin-ui/sheet";

import { RunDrawerView } from "./run-drawer-view";
import { useAgentRunDetail, useRunActions } from "./use-agent-run-detail";

/**
 * 运行详情抽屉（右侧滑出，宽屏 760px，不写进 URL）：点行的那一刻就打开，内容区在接口返回前显示骨架。
 * 换一行重新加载；关闭动画播完后才清理（onCleared），退出过程中内容不会闪成空的。
 * @param open 是否打开
 * @param rid 要看的运行（编码串）；关闭动画期间保留
 * @param onClose 点遮罩、按 Esc 或关闭按钮
 * @param onCleared 关闭动画播完，上层可以清掉 rid
 */
export function RunDrawer({
  open,
  rid,
  onClose,
  onCleared,
}: {
  open: boolean;
  rid: string | null;
  onClose: () => void;
  onCleared: () => void;
}) {
  const { state, retry } = useAgentRunDetail(rid);
  const actions = useRunActions(rid, state.detail);
  return (
    <Sheet
      open={open}
      onOpenChange={(next) => !next && onClose()}
      onOpenChangeComplete={(next) => !next && onCleared()}
    >
      <SheetContent className="gap-0 data-[side=right]:sm:max-w-[760px]">
        <SheetHeader className="border-b pr-14">
          <SheetTitle>运行详情</SheetTitle>
          <SheetDescription className="flex items-center gap-1">
            {rid ? (
              <>
                <span className="truncate font-mono text-xs" title={rid}>
                  {rid}
                </span>
                <CopyButton text={rid} iconOnly label="复制运行 ID" />
              </>
            ) : (
              "加载中"
            )}
          </SheetDescription>
        </SheetHeader>
        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
          <RunDrawerView
            state={state}
            copying={actions.copying}
            onCopyMarkdown={() => void actions.copyMarkdown()}
            onDownload={actions.download}
            onRetry={retry}
          />
        </div>
      </SheetContent>
    </Sheet>
  );
}
