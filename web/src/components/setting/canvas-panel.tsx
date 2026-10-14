import { useSettingsStore, type CanvasBackground, type CanvasWheelMode } from "@/store";

import { Segmented, SettingRow } from "./setting-controls";

const BACKGROUND_OPTIONS: { value: CanvasBackground; label: string }[] = [
  { value: "dots", label: "点阵" },
  { value: "lines", label: "线条" },
  { value: "cross", label: "十字" },
  { value: "none", label: "无" },
];

const WHEEL_OPTIONS: { value: CanvasWheelMode; label: string }[] = [
  { value: "pan", label: "平移画布" },
  { value: "zoom", label: "缩放画布" },
];

/** 画布设置：底纹与滚轮 */
export function CanvasPanel() {
  const background = useSettingsStore((state) => state.background);
  const wheelMode = useSettingsStore((state) => state.wheelMode);
  const updateSettings = useSettingsStore((state) => state.updateSettings);

  return (
    <>
      <SettingRow title="背景网格" hint="画布底纹的样式">
        <Segmented
          value={background}
          options={BACKGROUND_OPTIONS}
          onChange={(value) => updateSettings("background", value)}
        />
      </SettingRow>

      <SettingRow title="滚轮行为" hint="滚轮直接平移，还是直接缩放">
        <Segmented
          value={wheelMode}
          options={WHEEL_OPTIONS}
          onChange={(value) => updateSettings("wheelMode", value)}
        />
      </SettingRow>
    </>
  );
}
