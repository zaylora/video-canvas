import { beforeEach, describe, expect, test } from "bun:test";

import { usePetStore } from "@/store/pet";
import { initialBubbleQueue, type BubbleInput } from "@/utils/pet/bubble-queue";
import { reactionSpan } from "@/utils/pet/mood";

let seq = 0;
const make = (patch: Partial<BubbleInput> = {}): BubbleInput => ({
  id: `s${++seq}`,
  source: "system",
  priority: "system",
  text: "文案",
  mood: "idle",
  ttl: 4000,
  ...patch,
});
const st = () => usePetStore.getState();

beforeEach(() => {
  usePetStore.setState({
    active: false,
    status: "off",
    queue: initialBubbleQueue(),
    reaction: null,
  });
});

describe("usePetStore", () => {
  test("初始：不接管提醒，素材状态是 off，没有气泡，没有反应", () => {
    expect(st().active).toBe(false);
    expect(st().status).toBe("off");
    expect(st().queue.current).toBeNull();
    expect(st().reaction).toBeNull();
  });

  test("setStatus 记录素材加载状态，供外壳决定画不画胶囊", () => {
    st().setStatus("loading");
    expect(st().status).toBe("loading");
    st().setStatus("failed");
    expect(st().status).toBe("failed");
  });

  test("push 进入队列并显示，dismiss 关闭", () => {
    const b = make();
    st().push(b, 100);
    expect(st().queue.current?.id).toBe(b.id);
    st().dismiss(undefined, 200);
    expect(st().queue.current).toBeNull();
  });

  test("setContext 写入界面层的上下文，并驱动队列规则（浮窗打开收起助手气泡）", () => {
    st().push(make({ source: "assistant", priority: "approval" }), 0);
    expect(st().queue.current?.source).toBe("assistant");
    st().setContext({ panelOpen: true }, 1);
    expect(st().queue.current).toBeNull();
    expect(st().queue.ctx.panelOpen).toBe(true);
  });

  test("react：开始一次反应，播放期间同类再来保持不变", () => {
    st().react("celebrate", 1000);
    const first = st().reaction;
    expect(first).toEqual({ kind: "celebrate", until: 1000 + reactionSpan("celebrate") });
    st().react("celebrate", 1200);
    expect(st().reaction).toBe(first);
    st().react("failed", 1300);
    expect(st().reaction?.kind).toBe("failed");
  });

  test("clearReaction 清掉反应", () => {
    st().react("failed", 0);
    st().clearReaction();
    expect(st().reaction).toBeNull();
  });

  test("气泡变成当前时，按它的动作触发反应：jumping 庆祝、failed 失败，idle 不触发", () => {
    st().push(make({ mood: "idle" }), 0);
    expect(st().reaction).toBeNull();
    st().dismiss(undefined, 1);
    st().push(make({ mood: "jumping" }), 10);
    expect(st().reaction?.kind).toBe("celebrate");
    st().dismiss(undefined, 11);
    st().push(make({ mood: "failed" }), 20);
    expect(st().reaction?.kind).toBe("failed");
  });

  test("排队中的气泡不触发反应，轮到它显示时才触发", () => {
    st().push(make({ priority: "error" }), 0);
    st().push(make({ mood: "jumping" }), 1);
    expect(st().reaction).toBeNull();
    st().dismiss(undefined, 2);
    expect(st().reaction?.kind).toBe("celebrate");
  });

  test("setActive(false) 清掉所有气泡，但保留界面上下文", () => {
    st().setActive(true);
    st().setContext({ mode: "quiet" }, 0);
    st().push(make(), 1);
    st().setActive(false);
    expect(st().active).toBe(false);
    expect(st().queue.current).toBeNull();
    expect(st().queue.ctx.mode).toBe("quiet");
  });
});
