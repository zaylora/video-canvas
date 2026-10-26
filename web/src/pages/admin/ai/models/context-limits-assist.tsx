import { useEffect, useRef, useState } from "react";
import { Sparkles } from "lucide-react";

import { getModelLimits } from "@/api/admin/ai";
import type { ModelLimits } from "@/api/admin/ai/type.d";
import { Button } from "@/components/ui/button";
import { limitsDiffer, shouldAutoFill, sourceLabel } from "@/utils/admin/model-limits";

type Context = { window: number; output: number } | undefined;

/** 识别的进度：还没有上游模型名 / 识别中 / 失败 / 有结果（结果里 found 可能为 false） */
export type LimitsState =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "error" }
  | { kind: "ready"; limits: ModelLimits };

/** 上游模型名停止输入多久后才去识别 */
const DEBOUNCE_MS = 400;

const fmt = (n: number) => n.toLocaleString("en-US");

/**
 * 按上游模型名（和所在渠道）识别上下文规格：停止输入一会儿后请求一次，名字或渠道变了就重新识别。
 * 识别失败不是错误，运营照样可以手填。
 */
function useModelLimits(upstreamModel: string, channel: string): LimitsState {
  const name = upstreamModel.trim();
  const key = `${channel}\n${name}`;
  const [done, setDone] = useState<{ key: string; state: LimitsState } | null>(null);

  useEffect(() => {
    if (!name) return;
    let alive = true;
    const timer = setTimeout(() => {
      getModelLimits(name, channel || undefined)
        .then((limits) => alive && setDone({ key, state: { kind: "ready", limits } }))
        .catch(() => alive && setDone({ key, state: { kind: "error" } }));
    }, DEBOUNCE_MS);
    return () => {
      alive = false;
      clearTimeout(timer);
    };
  }, [name, channel, key]);

  if (!name) return { kind: "idle" };
  return done?.key === key ? done.state : { kind: "loading" };
}

/** 识别结果的提示行（纯展示）：说明识别到了什么，和当前值不同时给出「应用识别值」 */
export function ContextLimitsHintView({
  state,
  current,
  onApply,
}: {
  state: LimitsState;
  current: Context;
  onApply: (next: { window: number; output: number }) => void;
}) {
  const muted = "text-muted-foreground mt-2 text-xs";
  if (state.kind === "idle")
    return <p className={muted}>选好渠道并填写上游模型名后，会自动识别上下文窗口和最大输出。</p>;
  if (state.kind === "loading") return <p className={muted}>正在识别上下文规格…</p>;
  if (state.kind === "error" || !state.limits.found)
    return <p className={muted}>没能识别出这个模型的规格，请手动填写。</p>;

  const { limits } = state;
  return (
    <div className="text-muted-foreground mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
      <span className="inline-flex items-center gap-1">
        <Sparkles className="size-3.5" />
        {sourceLabel(limits.source)}：上下文 {fmt(limits.context_window)} · 最大输出{" "}
        {fmt(limits.max_output)}
      </span>
      {limitsDiffer(limits, current) && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => onApply({ window: limits.context_window, output: limits.max_output })}
        >
          应用识别值
        </Button>
      )}
    </div>
  );
}

/**
 * 上下文规格自动识别：认出来、并且运营还一项都没填时，自动填进表单（每个渠道 + 模型名组合只自动填一次）；
 * 已经填过的值不会被覆盖，只给出「应用识别值」按钮让运营自己决定。
 */
export function ContextLimitsAssist({
  channel,
  upstreamModel,
  current,
  onApply,
}: {
  channel: string;
  upstreamModel: string;
  current: Context;
  onApply: (next: { window: number; output: number }) => void;
}) {
  const state = useModelLimits(upstreamModel, channel);
  // 效果里要读最新的当前值和回调，但它们变化不该触发重新自动填
  const latest = useRef({ current, onApply });
  useEffect(() => {
    latest.current = { current, onApply };
  });
  const filledFor = useRef("");

  useEffect(() => {
    if (state.kind !== "ready" || !state.limits.found) return;
    const key = `${channel}\n${upstreamModel.trim()}`;
    if (filledFor.current === key) return;
    filledFor.current = key;
    if (shouldAutoFill(latest.current.current))
      latest.current.onApply({
        window: state.limits.context_window,
        output: state.limits.max_output,
      });
  }, [state, channel, upstreamModel]);

  return <ContextLimitsHintView state={state} current={current} onApply={onApply} />;
}
