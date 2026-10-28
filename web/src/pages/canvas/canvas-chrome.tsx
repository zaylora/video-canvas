import { ChromeZone } from "@/components/canvas/chrome/chrome";
import { useCanvasEditor } from "@/components/canvas-editor/editor-api";
import { useAgentNotifications } from "@/hooks/use-agent-notifications";
import { usePageTitle } from "@/hooks/use-page-title";
import { watchTitlePrefix } from "@/utils/agent/watch";
import { canvasPageTitle } from "@/utils/page-title";

import { useAgentHost } from "./agent-host";
import { AgentLauncher } from "./chrome/agent-launcher";
import { BottomToolbar } from "./chrome/bottom-toolbar";
import { EmptyState } from "./chrome/empty-state";
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
          onOpenCharacters={editor.openCharacterPicker}
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
        <AgentLauncher
          open={agent.open}
          available={agent.models === null ? null : agent.models.length > 0}
          running={agent.ctl.busy}
          onToggle={agent.toggle}
        />
        <div className="max-md:hidden">
          <StatsBar />
        </div>
      </ChromeZone>
      <ShortcutsDialog open={shortcutsOpen} onOpenChange={setShortcutsOpen} />
    </>
  );
}
