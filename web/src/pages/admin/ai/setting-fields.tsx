import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import type {
  SettingField,
  SettingFormValue,
  SettingFormValues,
} from "@/utils/admin/settings-form";

import { FormField } from "@/components/admin-ui/form-field";
import { OptionSelect } from "@/components/admin-ui/option-select";

/**
 * 按插件声明（channelSettings，即模型的协议设置项）渲染的表单字段，顺序即声明顺序。
 * 只管展示，取值与校验由调用方用 utils/admin/settings-form 处理。
 */
export function SettingFields({
  idPrefix,
  fields,
  values,
  errors,
  disabled,
  onChange,
}: {
  idPrefix: string;
  fields: SettingField[];
  values: SettingFormValues;
  errors?: Record<string, string>;
  disabled?: boolean;
  onChange: (name: string, value: SettingFormValue) => void;
}) {
  if (fields.length === 0) return null;
  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
      {fields.map((field) => {
        const id = `${idPrefix}-${field.name}`;
        const value = values[field.name];
        const error = errors?.[field.name];
        const hint = field.description ?? <span className="font-mono">{field.name}</span>;

        if (field.type === "boolean") {
          return (
            <FormField key={field.name} label={field.label} htmlFor={id} hint={hint} error={error}>
              <div className="flex h-9 items-center">
                <Switch
                  id={id}
                  checked={value === true}
                  disabled={disabled}
                  onCheckedChange={(checked) => onChange(field.name, checked)}
                />
              </div>
            </FormField>
          );
        }

        if (field.type === "enum") {
          return (
            <FormField
              key={field.name}
              label={field.label}
              htmlFor={id}
              hint={hint}
              error={error}
              required={field.required}
            >
              <OptionSelect
                id={id}
                value={typeof value === "string" ? value : ""}
                disabled={disabled}
                invalid={!!error}
                onValueChange={(next) => onChange(field.name, next)}
                options={[
                  { value: "", label: field.required ? "请选择" : "（不设置）" },
                  ...field.options.map((option) => ({ value: option, label: option })),
                ]}
              />
            </FormField>
          );
        }

        return (
          <FormField
            key={field.name}
            label={field.label}
            htmlFor={id}
            hint={hint}
            error={error}
            required={field.required}
          >
            <Input
              id={id}
              type={field.type === "number" ? "number" : "text"}
              inputMode={field.type === "number" ? "decimal" : undefined}
              value={typeof value === "string" ? value : ""}
              disabled={disabled}
              aria-invalid={!!error}
              onChange={(event) => onChange(field.name, event.target.value)}
            />
          </FormField>
        );
      })}
    </div>
  );
}
