/**
 * 需要在消息流里提示用户的运行状态：标题，以及一句「接下来可以做什么」。
 * 文字随 run.status 事件下发，前端不写死：改文案、加状态只改这里。和 Go 的 runStatusTexts 一致。
 */
const TEXTS: Record<string, { title: string; hint: string }> = {
  failed: { title: "运行失败", hint: "可以换个说法重试" },
  timeout: { title: "运行超时", hint: "可以继续，或换个说法重试" },
  interrupted: { title: "运行被中断", hint: "服务重启或运行进程退出了，可以从断点继续" },
  budget_exhausted: { title: "本轮额度用完了", hint: "追加额度后可以继续" },
  step_limit: { title: "达到本轮步数上限", hint: "继续的话会再给一轮步数" },
  canceled: { title: "已停止", hint: "没做完的计划还留着，可以接着让它做" },
  expired: { title: "等待超时", hint: "你太久没有回应，这一轮已经结束" },
};

export const ACTIVE_STATUSES = ["queued", "running", "waiting_approval", "waiting_input"] as const;
export const isActive = (s: string): boolean => (ACTIVE_STATUSES as readonly string[]).includes(s);

/** run.status 事件的内容：需要提示用户的状态带上 title 和 hint，extra 是附加字段（error、resumed 等） */
export function statusEvent(status: string, extra: Record<string, unknown> = {}): Record<string, unknown> {
  const t = TEXTS[status];
  return { status, ...(t && { title: t.title, hint: t.hint }), ...extra };
}
