/** 用量提示的档位：越高越该提醒用户，Agent 会在接近上限时自动压缩较早的对话 */
export type ContextTone = "normal" | "warn" | "danger";

/** 占比到这里变警示色 */
const WARN_RATIO = 0.75;
/** 占比到这里变危险色 */
const DANGER_RATIO = 0.9;

/** Token 数的简写：999、1.5K、12K、1.3M；负数和非数字当 0 */
export function formatTokens(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "0";
  if (n < 1000) return String(Math.round(n));
  if (n < 1_000_000) {
    const k = n / 1000;
    const rounded = k < 10 ? Math.round(k * 10) / 10 : Math.round(k);
    // 四舍五入后凑满 1000K 的，直接按 M 显示
    if (rounded < 1000) return `${rounded}K`;
  }
  return `${Math.round((n / 1_000_000) * 10) / 10}M`;
}

/** 上下文用量：已用占窗口的比例和档位。窗口没配（0）时只知道已用，占比按 0、不报警。 */
export function contextUsage(tokens: number, window: number) {
  const used = Number.isFinite(tokens) && tokens > 0 ? tokens : 0;
  const known = window > 0;
  const ratio = known ? Math.min(1, used / window) : 0;
  // 有用量就至少显示 1%，免得刚开始对话时显示成 0% 像是没算
  const percent = known && used > 0 ? Math.max(1, Math.round(ratio * 100)) : 0;
  const tone: ContextTone =
    ratio >= DANGER_RATIO ? "danger" : ratio >= WARN_RATIO ? "warn" : "normal";
  return { used, known, ratio, percent, tone };
}
