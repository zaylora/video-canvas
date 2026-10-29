import { Check } from "lucide-react";

import type { ModelInfo } from "@/api/model/type";
import { ModelLogo } from "@/components/model-logo";
import { useRemoteModels } from "@/hooks/use-models";
import { cn } from "@/lib/utils";

import { ChipPopover } from "./pickers";

/**
 * 短剧流水线用的 Agent 模型：value 为空串表示跟随后台第一个（默认）。
 * @returns 实际生效的模型（还没加载到为 undefined）、显示名，以及选的模型是否已停用
 */
export function useAgentModel(value: string): {
  models: ModelInfo[];
  model: ModelInfo | undefined;
  label: string;
  stale: boolean;
  loading: boolean;
} {
  const remote = useRemoteModels("agent");
  const first = remote.models[0];
  const model = value ? remote.models.find((m) => m.key === value) : first;
  const stale = !!value && remote.status === "ready" && !model;
  const label = stale
    ? `${value}（已停用）`
    : (model?.label ?? (value || first?.label || "Agent 模型"));
  return {
    models: remote.models,
    model,
    label,
    stale,
    loading: remote.status === "idle" || remote.status === "loading",
  };
}

/**
 * 选 Agent 模型的下拉（首页新建短剧用）：列后台「Agent」类别的模型，前面是厂商 logo，第一个标「默认」；
 * 选第一个等于跟随后台（回调空串），后台调整默认后自动跟着变。
 */
export function AgentModelPicker({
  value,
  onChange,
  variant = "chip",
}: {
  value: string;
  onChange: (key: string) => void;
  variant?: "chip" | "chrome";
}) {
  const { models, model, label, stale, loading } = useAgentModel(value);
  return (
    <ChipPopover
      variant={variant}
      icon={
        model &&
        !stale && <ModelLogo vendor={model.vendor} name={label} className="size-4 rounded-[5px]" />
      }
      label={label}
      width="w-72"
    >
      {(close) => (
        <>
          {models.length === 0 && (
            <div className="text-muted-foreground px-2 py-3 text-sm">
              {loading ? "加载中…" : "暂无可用的 Agent 模型"}
            </div>
          )}
          {models.map((item, index) => {
            const active = model?.key === item.key;
            return (
              <button
                key={item.key}
                type="button"
                onClick={() => {
                  close();
                  onChange(index === 0 ? "" : item.key);
                }}
                className={cn(
                  "hover:bg-muted focus-visible:ring-ring/50 flex min-h-8 items-center gap-2 rounded-md px-2 text-left text-sm outline-none focus-visible:ring-2",
                  active && "bg-muted font-medium",
                )}
              >
                <ModelLogo vendor={item.vendor} name={item.label} className="size-5" />
                <span className="min-w-0 flex-1 truncate">{item.label}</span>
                {index === 0 && (
                  <span className="bg-muted text-muted-foreground rounded-full px-1.5 text-[10px] leading-4">
                    默认
                  </span>
                )}
                {active && <Check className="size-4 shrink-0" />}
              </button>
            );
          })}
        </>
      )}
    </ChipPopover>
  );
}
