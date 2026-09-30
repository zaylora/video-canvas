import { useState, type ReactNode } from "react";
import { KeyRound, Pencil, Plus, Server, Trash2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

import { useSettingsStore, type CustomModel } from "@/store";

import { Segmented } from "./setting-controls";

/** 可挂自定义模型的节点种类 */
export type CustomModelKind = {
  kind: string;
  label: string;
  icon?: ReactNode;
};

/** 新建时的空表单，也是「取消」要还原成的样子 */
const EMPTY_FORM: Omit<CustomModel, "id"> = {
  label: "",
  kind: "",
  endpoint: "",
  apiKey: "",
  modelId: "",
  credits: 1,
};

type FormState = {
  /** 正在改的那条的 id，新建时为 null */
  id: string | null;
  values: Omit<CustomModel, "id">;
};

/** 一个表单字段：上面标题，下面控件 */
function Field({
  label,
  htmlFor,
  children,
}: {
  label: string;
  htmlFor?: string;
  children: ReactNode;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <Label htmlFor={htmlFor} className="text-muted-foreground text-xs">
        {label}
      </Label>
      {children}
    </div>
  );
}

type CustomModelPanelProps = {
  kinds: readonly CustomModelKind[];
};

/** 自定义模型：把自己的服务接进来，接好的模型会出现在节点的模型下拉里 */
export function CustomModelPanel({ kinds }: CustomModelPanelProps) {
  const customModels = useSettingsStore((state) => state.customModels);
  const addCustomModel = useSettingsStore((state) => state.addCustomModel);
  const updateCustomModel = useSettingsStore((state) => state.updateCustomModel);
  const removeCustomModel = useSettingsStore((state) => state.removeCustomModel);
  const [form, setForm] = useState<FormState | null>(null);

  const kindOptions = kinds.map((item) => ({
    value: item.kind,
    label: item.label,
  }));

  const openForm = (model?: CustomModel) => {
    setForm(
      model
        ? { id: model.id, values: { ...model } }
        : { id: null, values: { ...EMPTY_FORM, kind: kinds[0]?.kind ?? "" } },
    );
  };

  const setValue = <K extends keyof Omit<CustomModel, "id">>(
    key: K,
    value: Omit<CustomModel, "id">[K],
  ) => {
    setForm((current) =>
      current ? { ...current, values: { ...current.values, [key]: value } } : current,
    );
  };

  if (form) {
    const { id, values } = form;
    // 名字、地址、模型标识缺一个都调不起来，凑齐才让存
    const canSave = !!values.label.trim() && !!values.endpoint.trim() && !!values.modelId.trim();

    const save = () => {
      const payload = {
        ...values,
        label: values.label.trim(),
        endpoint: values.endpoint.trim(),
        modelId: values.modelId.trim(),
        apiKey: values.apiKey.trim(),
      };

      if (id) updateCustomModel(id, payload);
      else addCustomModel(payload);

      setForm(null);
    };

    return (
      <div className="flex flex-col gap-5">
        <div className="rounded-xl border bg-muted/20 p-4">
          <div className="mb-4 flex items-center gap-2">
            <div className="bg-primary/10 text-primary flex size-8 items-center justify-center rounded-lg">
              <Server className="size-4" />
            </div>
            <div>
              <p className="text-sm font-medium">服务信息</p>
              <p className="text-muted-foreground text-xs">填写模型的基本信息与接口地址</p>
            </div>
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <Field label="名称" htmlFor="custom-model-label">
              <Input
                id="custom-model-label"
                value={values.label}
                placeholder="我的图片模型"
                onChange={(event) => setValue("label", event.target.value)}
              />
            </Field>
            <Field label="模型标识" htmlFor="custom-model-id">
              <Input
                id="custom-model-id"
                value={values.modelId}
                placeholder="gpt-image-1"
                onChange={(event) => setValue("modelId", event.target.value)}
              />
            </Field>
          </div>

          <div className="mt-4">
            <Field label="接口地址" htmlFor="custom-model-endpoint">
              <Input
                id="custom-model-endpoint"
                value={values.endpoint}
                placeholder="https://api.example.com/v1/images"
                onChange={(event) => setValue("endpoint", event.target.value)}
              />
            </Field>
          </div>

          <div className="mt-4">
            <Field label="API Key" htmlFor="custom-model-key">
              <Input
                id="custom-model-key"
                type="password"
                value={values.apiKey}
                placeholder="只存在本机浏览器里"
                onChange={(event) => setValue("apiKey", event.target.value)}
              />
            </Field>
          </div>
        </div>

        <div className="rounded-xl border bg-muted/20 p-4">
          <div className="mb-4 flex items-center gap-2">
            <div className="bg-primary/10 text-primary flex size-8 items-center justify-center rounded-lg">
              <KeyRound className="size-4" />
            </div>
            <div>
              <p className="text-sm font-medium">使用范围</p>
              <p className="text-muted-foreground text-xs">设置模型可用于哪些节点</p>
            </div>
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-[1fr_8rem]">
            <Field label="适用节点">
              <Segmented
                value={values.kind}
                options={kindOptions}
                onChange={(value) => setValue("kind", value)}
              />
            </Field>
            <Field label="单次积分" htmlFor="custom-model-credits">
              <Input
                id="custom-model-credits"
                type="number"
                min={0}
                value={values.credits}
                onChange={(event) => setValue("credits", Number(event.target.value) || 0)}
              />
            </Field>
          </div>
        </div>

        <div className="flex justify-end gap-2 border-t pt-4">
          <Button variant="ghost" size="sm" onClick={() => setForm(null)}>
            取消
          </Button>
          <Button size="sm" disabled={!canSave} onClick={save}>
            {id ? "保存" : "添加"}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      {customModels.length === 0 && (
        <p className="text-muted-foreground border-border rounded-lg border border-dashed px-3 py-6 text-center text-xs">
          还没有接入自定义模型
        </p>
      )}

      {customModels.map((model) => {
        const kind = kinds.find((item) => item.kind === model.kind);

        return (
          <div
            key={model.id}
            className="group flex items-center gap-3 rounded-xl border bg-muted/15 p-3 transition-colors hover:bg-muted/30"
          >
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">{model.label}</div>
              <div className="text-muted-foreground truncate text-xs">
                {kind?.label ?? model.kind} · {model.credits} 积分 · {model.modelId}
              </div>
            </div>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={`编辑 ${model.label}`}
              onClick={() => openForm(model)}
            >
              <Pencil />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={`删除 ${model.label}`}
              onClick={() => removeCustomModel(model.id)}
            >
              <Trash2 />
            </Button>
          </div>
        );
      })}

      <Button variant="outline" size="sm" className="self-start" onClick={() => openForm()}>
        <Plus />
        添加模型
      </Button>
    </div>
  );
}
