import type { ModelLimits } from "@/api/admin/ai/type.d";

/** 表单里的上下文规格；模型还没配置过是 undefined */
type Context = { window: number; output: number } | undefined;

/** 只有运营一项都没填过（没有 context、或窗口和输出都是 0）才自动填，免得覆盖手填的值 */
export const shouldAutoFill = (current: Context) =>
  !current || (current.window === 0 && current.output === 0);

/** 识别出的规格和当前值是否不同：没认出来时没有可应用的值，恒为 false */
export const limitsDiffer = (limits: ModelLimits, current: Context) =>
  limits.found &&
  (current?.window !== limits.context_window || current?.output !== limits.max_output);

/** 规格来源的中文名；没有来源返回空串 */
export const sourceLabel = (source: ModelLimits["source"]) =>
  source === "upstream" ? "渠道上游返回" : source === "builtin" ? "内置模型库" : "";
