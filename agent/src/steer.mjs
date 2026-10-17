/** 运行中插话的处理：说明来意，并在模型正在输出回答时中止当前回答，让重答一次就把新旧两句话结合起来 */

const NOTE = "请结合之前的请求一起考虑，在同一次回复里回应，不要分开回答。";

/** 给插话加一层说明：不然模型会把它当成一个新问题单独回答 */
export function wrapSteer(text) {
  return `【用户在你执行任务时补充】${text}\n${NOTE}`;
}

/** pi 的用户消息 */
export function steerMessage(text) {
  return { role: "user", content: [{ type: "text", text: wrapSteer(text) }], timestamp: Date.now() };
}

/**
 * 跟踪 Agent 此刻是不是正在输出回答（assistant 的 message_start 到 message_end 之间）。
 * 这个区间里没有任何工具开始执行，中止它不会留下做到一半的画布改动；工具执行期间不打断，等这批工具跑完再注入。
 * 插话统一走 push：入队，必要时中止；被插话中止的运行由 runAgent 在 waitForIdle 之后接着处理（consumeInterrupt）。
 *
 * 不是收到插话就立刻中止，而是从第一条插话起等一个窗口：每次中止都会白扔一次已经付费的调用，
 * 用户连着补充几句时，窗口内到的合并成一次中止和重答。窗口到了回答还在输出才中止；回答自己结束了就不中止，
 * 插话留在队列里随下一轮注入。
 */
export const STEER_WINDOW_MS = 500;

export function createSteering(agent, { windowMs = STEER_WINDOW_MS } = {}) {
  let streaming = false;
  let interrupting = false;
  let timer = null;
  agent.subscribe((e) => {
    if (e.message?.role !== "assistant") return;
    if (e.type === "message_start") streaming = true;
    else if (e.type === "message_end") streaming = false;
  });
  return {
    push(text) {
      agent.steer(steerMessage(text));
      if (!streaming || interrupting || timer) return;
      timer = setTimeout(() => {
        timer = null;
        if (streaming && !interrupting) {
          interrupting = true;
          agent.abort();
        }
      }, windowMs);
      timer.unref?.();
    },
    /** 取消还没到点的中止（用户点了停止，或运行已经结束） */
    dispose() {
      clearTimeout(timer);
      timer = null;
    },
    /** 这次运行是不是被插话中止的；读一次就清掉标记 */
    consumeInterrupt() {
      const was = interrupting;
      interrupting = false;
      return was;
    },
  };
}
