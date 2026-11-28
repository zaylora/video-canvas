import NumberFlow from "@number-flow/react";
import { useReactFlow, useStore } from "@xyflow/react";
import type { ReactNode } from "react";
import {
  CircleDashed,
  CircleHelp,
  Grip,
  Map as MapIcon,
  Search,
  SlidersHorizontal,
} from "lucide-react";

import {
  ChromeButton,
  ChromePill,
  ChromeSegment,
  ChromeSeparator,
  ChromeTooltip,
} from "@/components/canvas/chrome/chrome";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useSettingsStore, type CanvasBackground, type CanvasWheelMode } from "@/store";
import type { ThemeSetting } from "@/lib/theme";

import { MOD } from "@/components/canvas-editor/keys";
import { VIEWPORT_DURATION } from "@/components/canvas-editor/viewport";

/** 左下角：工具切换、背景样式、缩放、小地图、快捷键帮助 */
export function ViewControls({
  minimap,
  preferences,
  toolControls,
  onMinimapChange,
  onOpenShortcuts,
  onLocalViewChange,
}: {
  minimap: boolean;
  preferences?: "editor" | "viewer";
  toolControls?: ReactNode;
  onMinimapChange: (minimap: boolean) => void;
  onOpenShortcuts?: () => void;
  onLocalViewChange?: () => void;
}) {
  const zoom = useStore((state) => state.transform[2]);
  const { zoomIn, zoomOut, zoomTo, fitView } = useReactFlow();
  const background = useSettingsStore((state) => state.background);
  const updateSettings = useSettingsStore((state) => state.updateSettings);
  const changeView = (change: () => Promise<boolean>) => {
    onLocalViewChange?.();
    void change();
  };

  return (
    <>
      <ChromePill>
        {toolControls && (
          <>
            {toolControls}
            <ChromeSeparator />
          </>
        )}
        {preferences ? (
          <ViewPreferences edit={preferences === "editor"} />
        ) : (
          <ChromeSegment<CanvasBackground>
            id="canvas-background"
            value={background}
            onValueChange={(value) => updateSettings("background", value)}
            items={[
              { value: "dots", label: "点阵背景", icon: <Grip /> },
              { value: "none", label: "无背景", icon: <CircleDashed /> },
            ]}
          />
        )}
        <ChromeSeparator />
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger
            render={<ChromeButton aria-label="缩放" className="min-w-18 font-mono text-xs" />}
          >
            <Search className="size-3.5!" />
            <NumberFlow value={Math.round(zoom * 100)} suffix="%" className="tabular-nums" />
          </DropdownMenuTrigger>
          <DropdownMenuContent side="top" sideOffset={12} className="w-48">
            <DropdownMenuItem
              onClick={() => changeView(() => zoomIn({ duration: VIEWPORT_DURATION }))}
            >
              放大
              {onOpenShortcuts && <DropdownMenuShortcut>{MOD}+</DropdownMenuShortcut>}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => changeView(() => zoomOut({ duration: VIEWPORT_DURATION }))}
            >
              缩小
              {onOpenShortcuts && <DropdownMenuShortcut>{MOD}−</DropdownMenuShortcut>}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={() =>
                changeView(() => fitView({ duration: VIEWPORT_DURATION, padding: 0.2 }))
              }
            >
              适应画布
              {onOpenShortcuts && <DropdownMenuShortcut>⇧1</DropdownMenuShortcut>}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => changeView(() => zoomTo(1, { duration: VIEWPORT_DURATION }))}
            >
              缩放到 100%
              {onOpenShortcuts && <DropdownMenuShortcut>⇧0</DropdownMenuShortcut>}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        <ChromeTooltip label={minimap ? "收起小地图" : "显示小地图"}>
          <ChromeButton
            aria-label="小地图"
            aria-pressed={minimap}
            active={minimap}
            onClick={() => onMinimapChange(!minimap)}
          >
            <MapIcon />
          </ChromeButton>
        </ChromeTooltip>
      </ChromePill>
      {onOpenShortcuts && (
        <ChromePill>
          <ChromeTooltip label="快捷键" shortcut="?">
            <ChromeButton aria-label="快捷键" onClick={onOpenShortcuts}>
              <CircleHelp />
            </ChromeButton>
          </ChromeTooltip>
        </ChromePill>
      )}
    </>
  );
}

const backgrounds = [
  ["dots", "点阵"],
  ["lines", "线条"],
  ["cross", "十字"],
  ["none", "无背景"],
] as const;

const themes = [
  ["light", "浅色"],
  ["dark", "深色"],
  ["system", "跟随系统"],
] as const;

function ViewPreferences({ edit }: { edit: boolean }) {
  const theme = useSettingsStore((state) => state.theme);
  const background = useSettingsStore((state) => state.background);
  const wheelMode = useSettingsStore((state) => state.wheelMode);
  const alignment = useSettingsStore((state) => state.collaborationAlignment);
  const update = useSettingsStore((state) => state.updateSettings);
  return (
    <DropdownMenu modal={false}>
      <ChromeTooltip label="视图设置">
        <DropdownMenuTrigger render={<ChromeButton aria-label="视图设置" />}>
          <SlidersHorizontal />
        </DropdownMenuTrigger>
      </ChromeTooltip>
      <DropdownMenuContent
        side="top"
        sideOffset={12}
        className="bg-chrome ring-chrome-border w-44 backdrop-blur-xl backdrop-saturate-150"
      >
        <DropdownMenuRadioGroup
          aria-label="主题"
          value={theme}
          onValueChange={(value) => update("theme", value as ThemeSetting)}
        >
          <DropdownMenuLabel>主题</DropdownMenuLabel>
          {themes.map(([value, label]) => (
            <DropdownMenuRadioItem key={value} value={value} closeOnClick>
              {label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuRadioGroup
          aria-label="背景"
          value={background}
          onValueChange={(value) => update("background", value as CanvasBackground)}
        >
          <DropdownMenuLabel>背景</DropdownMenuLabel>
          {backgrounds.map(([value, label]) => (
            <DropdownMenuRadioItem key={value} value={value} closeOnClick>
              {label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
        <DropdownMenuSeparator />
        <DropdownMenuRadioGroup
          aria-label="滚轮行为"
          value={wheelMode}
          onValueChange={(value) => update("wheelMode", value as CanvasWheelMode)}
        >
          <DropdownMenuLabel>滚轮行为</DropdownMenuLabel>
          <DropdownMenuRadioItem value="pan" closeOnClick>
            滚轮平移
          </DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="zoom" closeOnClick>
            滚轮缩放
          </DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
        {edit && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuCheckboxItem
              checked={alignment}
              closeOnClick
              onCheckedChange={(checked) => update("collaborationAlignment", checked)}
            >
              智能对齐与吸附
            </DropdownMenuCheckboxItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
