import { ChromeZone } from "@/components/canvas/chrome/chrome";
import { ActionContextMenu } from "@/components/action-context-menu";
import { useCanvasEditor } from "@/components/canvas-editor/editor-api";
import { ASSISTANT_NAME } from "@/constants/agent";
import { useAgentNotifications } from "@/hooks/use-agent-notifications";
import { usePageTitle } from "@/hooks/use-page-title";
import { cn } from "@/lib/utils";
import { useAgentSettings } from "@/store/agent-settings";
import { usePetStore } from "@/store/pet";
import { watchTitlePrefix } from "@/utils/agent/watch";
import { canvasPageTitle } from "@/utils/page-title";
import { cornerLayout } from "@/utils/pet/assets-status";

import { useAgentHost } from "./agent-host";
import { AgentLauncher } from "./chrome/agent-launcher";
import { BottomToolbar } from "./chrome/bottom-toolbar";
import { EmptyState } from "./chrome/empty-state";
import { PetLayer } from "./pet/pet-layer";
import { ShortcutsDialog } from "./chrome/shortcuts-dialog";
import { StatsBar } from "./chrome/stats-bar";
import { TopLeftBar } from "./chrome/top-left-bar";
import { TopRightBar } from "./chrome/top-right-bar";
import { ViewControls } from "./chrome/view-controls";
import { openShortcuts, useShortcutsDialog } from "./shortcuts-state";

/**
 * 无限画布页面的外壳：顶栏、底部工具条、空状态、视图控件、统计条、Agent 入口、快捷键说明，
 * 以及标签页标题（带 Agent 状态）。作为 CanvasEditor 的 overlay 插槽，状态和命令都从 useCanvasEditor() 取。
 */
export function CanvasChrome() {
  const editor = useCanvasEditor();
  const agent = useAgentHost();
  const shortcutsOpen = useShortcutsDialog((state) => state.open);
  const setShortcutsOpen = useShortcutsDialog((state) => state.setOpen);
  const { title } = editor;
  // 桌宠用它代替右下角的胶囊：素材加载中先留空位，不让胶囊闪一下；
  // 隐藏档、加载失败或等太久才显示胶囊
  const { showLauncher, reserveForPet } = cornerLayout(usePetStore((state) => state.status));
  const petHidden = useAgentSettings((state) => state.pet.mode === "hidden");
  // 标签页标题带上 Agent 的状态（待批准 / 后台等待进度），切到别的标签页也看得到；页面在后台时的系统通知同理
  usePageTitle(
    canvasPageTitle(title, watchTitlePrefix(agent.ctl.pending !== null, agent.ctl.watch)),
  );
  useAgentNotifications(editor.canvasId, title);

  return (
    <>
      {editor.nodes.length === 0 && (
        <EmptyState onAdd={editor.addAtCenter} onUpload={editor.uploadAtCenter} />
      )}

      <ChromeZone position="top-left">
        <TopLeftBar
          canvasId={editor.canvasId}
          title={title}
          onRename={editor.rename}
          saveStatus={editor.saveStatus}
          onSaveNow={editor.saveNow}
        />
      </ChromeZone>
      <ChromeZone position="top-right">
        <TopRightBar onOpenSettings={editor.openSettings} onOpenShortcuts={openShortcuts} />
      </ChromeZone>
      <ChromeZone position="left-center">
        <BottomToolbar
          tool={editor.tool}
          onToolChange={editor.setTool}
          onAdd={editor.addAtCenter}
          onUpload={editor.uploadAtCenter}
          onOpenAssets={editor.openAssetPicker}
          canUndo={editor.history.canUndo}
          canRedo={editor.history.canRedo}
          onUndo={editor.history.undo}
          onRedo={editor.history.redo}
        />
      </ChromeZone>
      <ChromeZone position="bottom-left" className="max-md:hidden">
        <ViewControls
          minimap={editor.minimap}
          onMinimapChange={editor.setMinimap}
          onOpenShortcuts={openShortcuts}
        />
      </ChromeZone>
      <ChromeZone position="bottom-right">
        {showLauncher && (
          // 隐藏档下右键胶囊可以把桌宠请回来
          <ActionContextMenu
            className="contents"
            actions={
              petHidden
                ? [
                    {
                      key: "show-pet",
                      label: `显示${ASSISTANT_NAME}`,
                      onSelect: () => useAgentSettings.getState().patchPet({ mode: "lively" }),
                    },
                  ]
                : []
            }
          >
            <AgentLauncher
              open={agent.open}
              available={agent.models === null ? null : agent.models.length > 0}
              running={agent.ctl.busy}
              onToggle={agent.toggle}
            />
          </ActionContextMenu>
        )}
        {/* 桌宠在右下角时，统计条往左让出 96px，不被压住 */}
        <div className={cn("max-md:hidden", reserveForPet && "mr-24")}>
          <StatsBar />
        </div>
      </ChromeZone>
      <PetLayer canvasId={editor.canvasId} />
      <ShortcutsDialog open={shortcutsOpen} onOpenChange={setShortcutsOpen} />
    </>
  );
}
