import { Plus } from "lucide-react";

import type { ConfigIssue } from "@/api/admin-ai/type";
import type { InputSchema } from "@/api/model/type";
import { FormField } from "@/components/admin-ui/form-field";
import {
  FormSection,
  FormSectionDescription,
  FormSectionHeader,
  FormSectionTitle,
} from "@/components/admin-ui/form-section";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { Tag } from "@/components/admin-ui/tag";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { readModelNumber, withModelField } from "@/utils/admin/model-body";

import { issueFor } from "./model-fields";
import { specFields } from "./model-previews";

type BodyMutator = (body: Record<string, unknown>) => Record<string, unknown> | null;

/** 后端目前只支持按次计费的统一价格（credits），其余定价能力先展示为“即将支持” */
const SOON = "即将支持";

/**
 * 积分定价页签（设计稿样式）：默认价格卡片（计费方式、每次消耗、积分成本）+ 规格价格。
 * 只有“按次 + 每次消耗积分”会写进 JSON，按秒 / Token、积分成本、规格价格先禁用。
 */
export function ModelPriceForm({
  body,
  kind,
  schema,
  issues,
  onChange,
}: {
  body: Record<string, unknown>;
  kind: string;
  schema: InputSchema | undefined;
  issues: ConfigIssue[];
  onChange: (mutate: BodyMutator) => void;
}) {
  const billing: Array<[string, string, string?]> = [
    ["per_call", "按次"],
    ["per_second", "按秒", kind === "video" ? `按秒计费${SOON}` : "只有视频按时长计费"],
    ["token", "Token", kind === "text" ? `按 Token 计费${SOON}` : "只有文本按 Token 计费"],
  ];
  const hasSpecs = specFields(schema).length > 0;

  return (
    <FormSection>
      <FormSectionHeader>
        <FormSectionTitle>用户积分价格</FormSectionTitle>
        <FormSectionDescription>
          默认只需一个统一价格；要按时长、分辨率等区分时，再加规格价格。
        </FormSectionDescription>
      </FormSectionHeader>
      <div className="rounded-xl border">
        <div className="flex items-center gap-3 border-b px-4 py-3">
          <span className="bg-muted grid size-8 place-items-center rounded-md font-mono text-xs">
            01
          </span>
          <div>
            <div className="text-sm font-semibold">默认价格</div>
            <div className="text-muted-foreground text-xs">没有命中规格价格的请求都用这个价格</div>
          </div>
        </div>
        <div className="flex flex-col gap-4 p-4">
          <FormField
            size="default"
            label="计费方式"
            required
            hint={`每生成一次扣一次。按秒、按 Token 计费${SOON}。`}
          >
            <Segmented aria-label="计费方式" className="w-fit">
              {billing.map(([value, label, reason]) => (
                <SegmentedItem
                  key={value}
                  active={value === "per_call"}
                  disabled={!!reason}
                  title={reason}
                >
                  {label}
                </SegmentedItem>
              ))}
            </Segmented>
          </FormField>
          <FormField
            size="default"
            label="每次消耗积分"
            htmlFor="model-credits"
            required
            error={issueFor(issues, "credits")}
          >
            <div className="relative">
              <Input
                id="model-credits"
                type="number"
                min={0}
                className="pr-16 tabular-nums"
                value={readModelNumber(body, "credits") ?? ""}
                aria-invalid={!!issueFor(issues, "credits")}
                onChange={(event) => {
                  const value = Number(event.target.value);
                  onChange((body) =>
                    withModelField(
                      body,
                      "credits",
                      event.target.value.trim() === "" || !Number.isFinite(value) ? 0 : value,
                    ),
                  );
                }}
              />
              <span className="text-muted-foreground pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-xs">
                积分/次
              </span>
            </div>
          </FormField>
          <div className="bg-muted/30 rounded-lg border p-3">
            <div className="flex items-center gap-2">
              <span className="text-sm font-medium">积分成本</span>
              <Tag>仅管理员可见</Tag>
              <Tag tone="info">{SOON}</Tag>
              <span className="text-muted-foreground hidden text-xs sm:inline">
                填了成本才能看到利润率，不影响用户价格。
              </span>
              <Switch className="ml-auto" checked={false} disabled aria-label="积分成本" />
            </div>
          </div>
        </div>
      </div>
      <button
        type="button"
        disabled
        title={hasSpecs ? `规格价格${SOON}` : "这个能力没有可区分的规格"}
        className="text-muted-foreground hover:bg-accent/40 hover:text-foreground mt-4 flex h-12 w-full items-center justify-center gap-2 rounded-xl border border-dashed text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
      >
        <Plus className="size-4" />
        新增规格价格
        {hasSpecs && <Tag tone="info">{SOON}</Tag>}
      </button>
    </FormSection>
  );
}
