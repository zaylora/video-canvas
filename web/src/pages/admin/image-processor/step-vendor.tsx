import { Cloud } from "lucide-react";

import type { ProcessorPreset, ProcessorVendor } from "@/api/admin/image-processor/type.d";
import { ChoiceCard, ChoiceCardGroup } from "@/components/admin-ui/choice-card";
import { Notice } from "@/components/admin-ui/notice";
import { Tag } from "@/components/admin-ui/tag";
import { isOwnStorageVendor, storageProviderLabel } from "@/utils/admin/image-processor";

/**
 * 第一步：选厂商。“必须使用自家存储”和“无视频封面”在这里就亮出来，不等用户选到一半才发现不行；
 * 名称、能力都按预设渲染。
 * @param presets 厂商预设
 * @param value 当前选中的厂商
 * @param disabled 整组禁用（只读，或已创建的处理服务不能换厂商）
 * @param onChange 选择厂商
 */
export function StepVendor({
  presets,
  value,
  disabled,
  onChange,
}: {
  presets: ProcessorPreset[];
  value: ProcessorVendor | null;
  disabled: boolean;
  onChange: (vendor: ProcessorVendor) => void;
}) {
  return (
    <div className="flex flex-col gap-3">
      <ChoiceCardGroup aria-label="处理服务厂商">
        {presets.map((preset) => (
          <ChoiceCard
            key={preset.vendor}
            indicator
            selected={preset.vendor === value}
            disabled={disabled}
            className="data-selected:disabled:opacity-100"
            onClick={() => onChange(preset.vendor)}
          >
            <span className="flex min-w-0 flex-1 flex-col gap-1.5">
              <span className="flex flex-wrap items-center gap-1.5 text-sm font-medium">
                <Cloud className="size-4 shrink-0" />
                {preset.name}
                {isOwnStorageVendor(preset.vendor) && <Tag tone="warning">必须使用自家存储</Tag>}
                {!preset.supports_poster && <Tag tone="danger">无视频封面</Tag>}
              </span>
              <span className="text-muted-foreground text-xs">
                只能绑定「{storageProviderLabel(preset.storage_provider)}」存储
                {preset.requires_public_base && "，且存储需设置公开域名"}。
              </span>
            </span>
          </ChoiceCard>
        ))}
      </ChoiceCardGroup>
      <Notice tone="neutral">
        “必须使用自家存储”的厂商，处理能力是存储桶自带的：下一步只能选对应厂商的存储。
      </Notice>
    </div>
  );
}
