import { Switch } from "@/components/ui/switch";

import type { ThemeSetting } from "@/lib/theme";
import { useSettingsStore } from "@/store";

import { Segmented, SettingRow } from "./setting-controls";

const THEME_OPTIONS: { value: ThemeSetting; label: string }[] = [
  { value: "system", label: "跟随系统" },
  { value: "light", label: "浅色" },
  { value: "dark", label: "深色" },
];

/** 通用设置：外观与提示 */
export function GeneralPanel() {
  const theme = useSettingsStore((state) => state.theme);
  const showHints = useSettingsStore((state) => state.showHints);
  const updateSettings = useSettingsStore((state) => state.updateSettings);

  return (
    <>
      <SettingRow title="主题" hint="锁定后不再跟随系统深浅色">
        <Segmented
          value={theme}
          options={THEME_OPTIONS}
          onChange={(value) => updateSettings("theme", value)}
        />
      </SettingRow>

      <SettingRow title="操作提示" hint="空画布时顶部那条快捷键提示">
        <Switch
          checked={showHints}
          onCheckedChange={(checked) => updateSettings("showHints", checked)}
        />
      </SettingRow>
    </>
  );
}
