import { describe, expect, test } from "bun:test";

import type { AgentEventDto, AgentSessionDto } from "@/api/agent/type";
import { AGENT_MODES, TOOL_LABELS } from "@/constants/agent";
import {
  DRAMA_REFRESH_TOOLS,
  dramaRefreshFor,
  editPayload,
  editTitle,
  visibleSessions,
} from "@/utils/agent/drama";

const toolEnd = (
  name: string,
  patch: Partial<AgentEventDto> = {},
  data: Record<string, unknown> = {},
): AgentEventDto => ({
  session_id: "s1",
  canvas_id: "c1",
  run_id: "r1",
  seq: 5,
  type: "tool.end",
  data: { id: "call1", name, is_error: false, ...data },
  created_at: "x",
  ...patch,
});

const session = (id: string, mode: AgentSessionDto["mode"]): AgentSessionDto => ({
  id,
  canvas_id: "c1",
  title: id,
  mode,
  model_key: "m",
  last_seq: 0,
  created_at: "x",
  updated_at: "x",
});

describe("助手工具结果触发的刷新", () => {
  test("改大纲 / 改集 / 改片段 / 提交任务成功后重新拉短剧详情", () => {
    for (const name of [
      "drama_update_outline",
      "drama_update_episode",
      "drama_update_segment",
      "drama_run_job",
    ]) {
      expect(dramaRefreshFor(toolEnd(name), "c1")).toBe(true);
    }
    expect(DRAMA_REFRESH_TOOLS).toContain("drama_update_episode");
  });

  test("只读工具、画布工具、别的画布、失败的调用都不刷新", () => {
    expect(dramaRefreshFor(toolEnd("drama_read"), "c1")).toBe(false);
    expect(dramaRefreshFor(toolEnd("canvas_apply_ops"), "c1")).toBe(false);
    expect(dramaRefreshFor(toolEnd("drama_update_episode"), "other")).toBe(false);
    expect(dramaRefreshFor(toolEnd("drama_update_episode", {}, { is_error: true }), "c1")).toBe(
      false,
    );
  });

  test("tool.start 等其他事件不刷新", () => {
    expect(dramaRefreshFor(toolEnd("drama_update_episode", { type: "tool.start" }), "c1")).toBe(
      false,
    );
  });
});

describe("AgentMode 扩展不影响画布", () => {
  test("画布 Agent 看不到短剧会话；短剧助手只看短剧会话", () => {
    const list = [session("a", "all"), session("b", "drama"), session("c", "script")];
    expect(visibleSessions(list, "all").map((s) => s.id)).toEqual(["a", "c"]);
    expect(visibleSessions(list, "drama").map((s) => s.id)).toEqual(["b"]);
  });

  test("没有 drama 会话时画布看到的列表与原列表完全相同（同一引用）", () => {
    const list = [session("a", "all")];
    expect(visibleSessions(list, "all")).toBe(list);
  });

  test("任务模式清单（画布不提供切换）没有加 drama，工具名有中文标签", () => {
    expect(AGENT_MODES.map((m) => m.value)).toEqual(["all", "script", "storyboard", "prompt"]);
    expect(TOOL_LABELS.drama_update_episode).toBeTruthy();
    expect(TOOL_LABELS.drama_run_job).toBeTruthy();
  });
});

describe("通用修改审批的内容", () => {
  test("editPayload 补齐缺省字段，before / after 取文本", () => {
    expect(
      editPayload({ title: "修改第 3 集剧本", before: "旧", after: "新", reason: "补伏笔" }),
    ).toEqual({
      title: "修改第 3 集剧本",
      reason: "补伏笔",
      before: "旧",
      after: "新",
    });
    expect(editPayload(null)).toEqual({ title: "", reason: "", before: "", after: "" });
    expect(editPayload({ before: 3 })).toEqual({ title: "", reason: "", before: "", after: "" });
  });

  test("editTitle：有标题用标题，没有给兜底", () => {
    expect(editTitle(editPayload({ title: "修改大纲" }))).toBe("修改大纲");
    expect(editTitle(editPayload({}))).toBe("申请修改");
  });
});
