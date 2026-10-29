import { memo } from "react";

import { CanvasEditor } from "@/components/canvas-editor/canvas-editor";
import type { CanvasDetailDto } from "@/api/canvas/type";
import type { Recovery } from "@/utils/canvas/draft-reconcile";

import { AgentDock, AgentHostProvider } from "./agent-host";
import { buildAddNodeItems } from "./chrome/add-node-items";
import { CanvasChrome } from "./canvas-chrome";
import { openShortcuts } from "./shortcuts-state";

/** 外壳的两个插槽元素摆在模块顶层，引用不变，Flow 重渲染时不会牵动编辑器 */
const overlay = <CanvasChrome />;
const aside = <AgentDock />;

/**
 * 无限画布页面：用 CanvasEditor 组装顶栏、底栏、Agent 这些页面专属的外壳。
 * 编辑器本体（节点、保存、撤销、弹窗……）在 components/canvas-editor，别的页面（如短剧资产画布）也这样各自组装。
 * 必须放在 ReactFlowProvider 里。
 */
export const Flow = memo(function Flow({
  canvas,
  recovery,
  onConflict,
}: {
  canvas: CanvasDetailDto;
  /** 打开时本地草稿的对账结果：restored 是已用草稿恢复，conflict 是草稿基于的版本已过期 */
  recovery: Recovery | null;
  onConflict: (canvas: CanvasDetailDto) => void;
}) {
  return (
    <AgentHostProvider canvasId={canvas.id}>
      <CanvasEditor
        canvas={canvas}
        recovery={recovery}
        onConflict={onConflict}
        menuItems={buildAddNodeItems}
        onOpenShortcuts={openShortcuts}
        overlay={overlay}
        aside={aside}
      />
    </AgentHostProvider>
  );
});
