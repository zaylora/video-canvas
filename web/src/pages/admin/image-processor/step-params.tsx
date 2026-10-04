import type { ProcessorConfig, ProcessorPreset } from "@/api/admin-image-processor/type.d";
import { FormField } from "@/components/admin-ui/form-field";
import { NativeSelect } from "@/components/admin-ui/native-select";
import { Notice } from "@/components/admin-ui/notice";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  configFields,
  isOwnStorageVendor,
  WIDTH_MAX,
  WIDTH_MIN,
  type ProcessorFormErrors,
} from "@/utils/admin/image-processor";

/** 数字输入框的取值：清空时是 NaN，由校验报错；显示时 NaN 还原成空串 */
const numberText = (value: number | undefined) =>
  value === undefined || Number.isNaN(value) ? "" : value;

/**
 * 第三步：参数表单。显示哪些字段由预设与厂商决定（configFields）；
 * 不支持封面的厂商不显示取帧时间并给出提示；校验错误提交过一次后才显示。
 * @param preset 厂商预设
 * @param storageName 绑定的存储名称（摘要）
 * @param name 处理服务名称
 * @param config 参数
 * @param errors 要显示的校验错误
 * @param published 是否是已发布的处理服务：保存的是草稿，发布后才生效
 * @param disabled 整个表单只读
 * @param onNameChange 改名称
 * @param onConfigChange 改参数（局部）
 */
export function StepParams({
  preset,
  storageName,
  name,
  config,
  errors,
  published,
  disabled,
  onNameChange,
  onConfigChange,
}: {
  preset: ProcessorPreset;
  storageName: string;
  name: string;
  config: ProcessorConfig;
  errors: ProcessorFormErrors;
  published: boolean;
  disabled: boolean;
  onNameChange: (name: string) => void;
  onConfigChange: (patch: Partial<ProcessorConfig>) => void;
}) {
  const fields = configFields(preset);
  const formats = preset.formats.includes(config.format)
    ? preset.formats
    : [config.format, ...preset.formats];

  return (
    <fieldset disabled={disabled} className="flex min-w-0 flex-col gap-4">
      <dl className="bg-muted/40 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 rounded-lg border px-3 py-2 text-sm">
        <dt className="text-muted-foreground">厂商</dt>
        <dd>{preset.name}</dd>
        <dt className="text-muted-foreground">绑定存储</dt>
        <dd>{storageName}</dd>
      </dl>

      {published && (
        <Notice tone="info" title="已发布的处理服务">
          这里保存的是草稿，重新校验并发布后才生效，线上配置不变。
        </Notice>
      )}

      <FormField label="名称" htmlFor="processor-name" error={errors.name} required>
        <Input
          id="processor-name"
          value={name}
          maxLength={64}
          aria-invalid={!!errors.name}
          onChange={(event) => onNameChange(event.target.value)}
        />
      </FormField>

      <FormField
        label={fields.domainLabel}
        htmlFor="processor-domain"
        error={errors.domain}
        hint={fields.domainHint}
        required
      >
        <Input
          id="processor-domain"
          className="font-mono"
          value={config.domain}
          placeholder="例如 img.example.com"
          aria-invalid={!!errors.domain}
          onChange={(event) => onConfigChange({ domain: event.target.value })}
        />
      </FormField>

      <div className="grid grid-cols-2 gap-3">
        <FormField
          label="缩略图长边（px）"
          htmlFor="processor-width"
          error={errors.width}
          hint={`${WIDTH_MIN}–${WIDTH_MAX}，默认 512`}
          required
        >
          <Input
            id="processor-width"
            type="number"
            className="tabular-nums"
            value={numberText(config.width)}
            aria-invalid={!!errors.width}
            onChange={(event) => onConfigChange({ width: event.target.valueAsNumber })}
          />
        </FormField>
        <FormField label="输出格式" htmlFor="processor-format">
          <NativeSelect
            id="processor-format"
            value={config.format}
            onChange={(event) => onConfigChange({ format: event.target.value })}
          >
            {formats.map((format) => (
              <option key={format} value={format}>
                {format}
              </option>
            ))}
          </NativeSelect>
        </FormField>
      </div>

      {fields.quality && (
        <FormField
          label="质量"
          htmlFor="processor-quality"
          error={errors.quality}
          hint="1–100，默认 75"
        >
          <Input
            id="processor-quality"
            type="number"
            className="tabular-nums"
            value={numberText(config.quality)}
            aria-invalid={!!errors.quality}
            onChange={(event) => onConfigChange({ quality: event.target.valueAsNumber })}
          />
        </FormField>
      )}

      {fields.time ? (
        <FormField
          label="视频封面取帧时间（秒）"
          htmlFor="processor-time"
          error={errors.time_sec}
          hint="AI 视频第 0 秒可能是黑帧，可以调到 0.5"
        >
          <Input
            id="processor-time"
            type="number"
            step="0.1"
            min={0}
            className="tabular-nums"
            value={numberText(config.time_sec)}
            aria-invalid={!!errors.time_sec}
            onChange={(event) => onConfigChange({ time_sec: event.target.valueAsNumber })}
          />
        </FormField>
      ) : (
        <Notice tone="danger" title="该厂商不支持视频封面">
          视频节点显示占位，点击仍可播放；图片缩略图不受影响。
        </Notice>
      )}

      {fields.mediaEnabled && (
        <SwitchRow
          id="processor-media"
          title="已开通数据万象“媒体处理”"
          hint="视频封面（截帧）需要单独开通并另行计费；未开通时没有视频封面。"
          checked={config.media_enabled ?? false}
          onChange={(checked) => onConfigChange({ media_enabled: checked })}
        />
      )}

      {fields.onErrorRedirect && (
        <SwitchRow
          id="processor-onerror"
          title="处理失败时回退原图"
          hint="处理失败（超限、格式不支持）时直接返回原图，而不是报错。"
          checked={config.on_error_redirect ?? true}
          onChange={(checked) => onConfigChange({ on_error_redirect: checked })}
        />
      )}

      {isOwnStorageVendor(preset.vendor) && (
        <Notice tone="info">
          私有桶签名直接使用「{storageName}」存储配置里已有的 AccessKey，这里不需要再填密钥。
        </Notice>
      )}
    </fieldset>
  );
}

function SwitchRow({
  id,
  title,
  hint,
  checked,
  onChange,
}: {
  id: string;
  title: string;
  hint: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-4 rounded-lg border p-3">
      <label htmlFor={id} className="min-w-0 text-sm">
        <span className="font-medium">{title}</span>
        <span className="text-muted-foreground mt-0.5 block text-xs">{hint}</span>
      </label>
      <Switch id={id} checked={checked} onCheckedChange={onChange} />
    </div>
  );
}
