import type { CloudProvider, StoragePreset } from "@/api/admin/storage/type.d";
import { ChoiceCard, ChoiceCardGroup } from "@/components/admin-ui/choice-card";
import { Skeleton } from "@/components/ui/skeleton";

import { PROVIDER_META } from "./provider-meta";

/**
 * 服务商卡片：名称与顺序来自预设接口；编辑时不可更改（服务商决定了素材在桶里的组织方式，创建后固定）。
 * @param presets 预设列表；还没加载时显示骨架
 * @param value 当前选中的服务商
 * @param disabled 整组禁用（只读，或编辑已有存储）
 * @param onChange 选择服务商
 */
export function ProviderPicker({
  presets,
  value,
  disabled,
  onChange,
}: {
  presets: StoragePreset[];
  value: CloudProvider;
  disabled: boolean;
  onChange: (provider: CloudProvider) => void;
}) {
  if (presets.length === 0) return <Skeleton className="h-16" />;
  return (
    <ChoiceCardGroup aria-label="服务商" className="grid-cols-2 sm:grid-cols-4">
      {presets.map((preset) => {
        const { icon: Icon, hint } = PROVIDER_META[preset.provider];
        return (
          <ChoiceCard
            key={preset.provider}
            selected={preset.provider === value}
            disabled={disabled}
            className="flex-col gap-0.5 p-2.5 data-selected:disabled:opacity-100"
            onClick={() => onChange(preset.provider)}
          >
            <span className="flex items-center gap-1.5 text-sm font-medium">
              <Icon className="size-4 shrink-0" />
              {preset.name}
            </span>
            <span className="text-muted-foreground text-xs">{hint}</span>
          </ChoiceCard>
        );
      })}
    </ChoiceCardGroup>
  );
}
