import { beforeEach, describe, expect, test } from "bun:test";

import type { AgentEventDto, AgentSessionDto } from "@/api/agent/type";
import { useAgentStore } from "@/store/agent";

const session = (id: string, title: string): AgentSessionDto =>
  ({
    id,
    canvas_id: "c1",
    title,
    mode: "all",
    model_key: "",
    last_seq: 0,
    created_at: "2026-10-26T00:00:00Z",
    updated_at: "2026-10-26T00:00:00Z",
  }) as AgentSessionDto;

const titled = (sessionId: string, title: unknown, canvasId = "c1"): AgentEventDto => ({
  session_id: sessionId,
  canvas_id: canvasId,
  run_id: "r1",
  seq: 0,
  type: "session.titled",
  data: { title },
  created_at: "2026-10-26T00:00:00Z",
});

const titles = () => (useAgentStore.getState().sessionsByCanvas.c1 ?? []).map((s) => s.title);

describe("Agent 会话标题事件", () => {
  beforeEach(() => {
    useAgentStore.setState({
      sessionsByCanvas: { c1: [session("s2", "第二个"), session("s1", "新对话")] },
      sessions: {},
    });
  });

  test("session.titled 把对应会话的标题改成服务端给的，位置和别的会话不变", () => {
    useAgentStore.getState().handleEvent(titled("s1", "雨夜便利店分镜"));
    expect(titles()).toEqual(["第二个", "雨夜便利店分镜"]);
  });

  test("临时事件不进会话的事件列表，也不会让会话状态被创建出来", () => {
    useAgentStore.getState().handleEvent(titled("s1", "雨夜便利店分镜"));
    expect(useAgentStore.getState().sessions.s1).toBeUndefined();
  });

  test("会话不在列表里（别的画布、还没加载）时什么都不改", () => {
    useAgentStore.getState().handleEvent(titled("nope", "x"));
    useAgentStore.getState().handleEvent(titled("s1", "别的画布的", "c2"));
    expect(titles()).toEqual(["第二个", "新对话"]);
  });

  test("标题缺失、为空或不是字符串时忽略", () => {
    for (const bad of [undefined, "", "   ", 42])
      useAgentStore.getState().handleEvent(titled("s1", bad));
    expect(titles()).toEqual(["第二个", "新对话"]);
  });
});
