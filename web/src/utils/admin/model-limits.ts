import type { ModelLimits } from "@/api/admin/ai/type.d";
import type { Capabilities } from "@/api/model/type.d";

/** 表单里的上下文规格；模型还没配置过是 undefined */
type Context = { window: number; output: number } | undefined;

/** 输入能力建议；未知项省略，非 Gemini 不应用看视频建议。仅在运营点击应用后使用。 */
export const suggestedMediaCapabilities = (limits: ModelLimits, gemini: boolean) => ({
  ...(limits.vision === undefined ? {} : { vision: limits.vision }),
  ...(gemini && limits.video !== undefined ? { video: limits.video } : {}),
});

/** 只有运营一项都没填过（没有 context、或窗口和输出都是 0）才自动填，免得覆盖手填的值 */
export const shouldAutoFill = (current: Context) =>
  !current || (current.window === 0 && current.output === 0);

/** 已识别的上下文或可应用的输入能力是否不同；未知项不覆盖当前配置。 */
export const limitsDiffer = (
  limits: ModelLimits,
  current: Context,
  media?: Pick<Capabilities, "vision" | "video">,
  gemini = false,
) =>
  (limits.found &&
    (current?.window !== limits.context_window || current?.output !== limits.max_output)) ||
  (!!media &&
    Object.entries(suggestedMediaCapabilities(limits, gemini)).some(
      ([key, value]) => !!media[key as "vision" | "video"] !== value,
    ));

/** 规格来源的中文名；没有来源返回空串 */
export const sourceLabel = (source: ModelLimits["source"]) =>
  source === "upstream" ? "渠道上游返回" : source === "builtin" ? "内置模型库" : "";
