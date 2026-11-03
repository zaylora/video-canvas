import { describe, expect, test } from "bun:test";

import { activitySummary, formatSeconds, toolIcon } from "@/utils/agent/activity";
import type { ToolCall } from "@/utils/agent/timeline";

const tool = (name: string, extra: Partial<ToolCall> = {}): ToolCall => ({
  key: `${name}-${Math.random()}`,
  name,
  status: "done",
  summary: "",
  nodeIds: [],
  ...extra,
});

describe("activitySummary：活动行收起后的图标摘要", () => {
  test("按动词首次出现的顺序计数，带图标键", () => {
    const got = activitySummary({
      tools: [
        tool("plan_update"),
        tool("canvas_get_state"),
        tool("plan_update"),
        tool("canvas_apply_ops"),
      ],
      status: "done",
      startedAt: 0,
      endedAt: 69_000,
    });
    expect(got.seconds).toBe(69);
    expect(got.verbs).toEqual([
      { verb: "计划", icon: "plan", count: 2 },
      { verb: "读取", icon: "read", count: 1 },
      { verb: "修改", icon: "edit", count: 1 },
    ]);
  });

  test("后端下发的动词优先；不认识的动词用通用图标", () => {
    const got = activitySummary({
      tools: [tool("codemode", { verb: "脚本" }), tool("new_tool", { verb: "冥想" })],
      status: "done",
      startedAt: null,
      endedAt: null,
    });
    expect(got.seconds).toBeNull();
    expect(got.verbs.map((v) => v.icon)).toEqual(["script", "other"]);
  });

  test("用时至少 1 秒", () => {
    const got = activitySummary({ tools: [], status: "done", startedAt: 0, endedAt: 200 });
    expect(got.seconds).toBe(1);
  });

  test("文字版摘要给读屏和 tooltip 用", () => {
    const got = activitySummary({
      tools: [tool("ask_user"), tool("canvas_apply_ops")],
      status: "aborted",
      startedAt: 0,
      endedAt: 27_000,
    });
    expect(got.text).toBe("已中止 · 提问 1 · 修改 1");
  });
});

describe("activitySummary：后台助手的工具不说成画布", () => {
  const sum = (tools: ToolCall[]) =>
    activitySummary({ tools, status: "done", startedAt: 0, endedAt: 12_000 });
  /** 后端 tool_registry 给后台工具的 label / verb */
  const admin = (name: string, label: string, verb: string) => tool(name, { label, verb });

  test("按工具说动作：读模型配置、预览价格、提交变更单", () => {
    const got = sum([
      admin("admin_get_model", "读取模型配置", "读取"),
      admin("admin_quote_preview", "预览价格变更", "算价"),
      admin("admin_quote_preview", "预览价格变更", "算价"),
      tool("plan_update"),
      admin("admin_propose_changes", "申请后台变更", "修改"),
    ]);
    expect(got.phrase).toBe("读取了模型配置、预览了价格 2 次、更新了计划等 4 项");
    expect(got.phrase).not.toContain("画布");
  });

  test("其余后台工具也有自己的说法", () => {
    expect(
      sum([
        admin("admin_list_models", "查看后台模型", "查模型"),
        admin("admin_get_pricing_settings", "读取定价设置", "读取"),
        admin("admin_propose_changes", "申请后台变更", "修改"),
      ]).phrase,
    ).toBe("查看了模型列表、读取了定价设置、提交了变更单");
    expect(sum([admin("admin_list_channels", "查看渠道", "查渠道")]).phrase).toBe("查看了渠道");
  });

  test("画布工具的说法不变", () => {
    expect(sum([tool("canvas_get_state"), tool("canvas_apply_ops")]).phrase).toBe(
      "读取了画布、修改了画布",
    );
  });

  test("二期插件全链路工具有后台动作短语", () => {
    for (const [name, phrase] of [
      ["admin_list_plugins", "查看了插件"],
      ["admin_get_plugin", "读取了插件代码"],
      ["admin_plugin_check", "验证了插件"],
      ["admin_request_secret", "请求填写密钥"],
      ["admin_test_run", "申请了试跑"],
    ])
      expect(sum([admin(name, name, "读取")]).phrase).toBe(phrase);
  });
});

describe("activitySummary：主图标和兜底动作短语", () => {
  const sum = (tools: ToolCall[], status: "done" | "aborted" = "done") =>
    activitySummary({ tools, status, startedAt: 0, endedAt: 12_000 });

  test("主图标取优先级最高的动作：生成 > 修改 > 搜索 > 读取", () => {
    expect(sum([tool("canvas_get_state"), tool("web_search"), tool("canvas_apply_ops")]).icon).toBe(
      "edit",
    );
    expect(sum([tool("canvas_get_state"), tool("web_search")]).icon).toBe("search");
    expect(sum([tool("canvas_apply_ops"), tool("generate_media")]).icon).toBe("generate");
    expect(sum([tool("canvas_get_state")]).icon).toBe("read");
  });

  test("兜底短语：按首次出现的顺序，同一动作多次写「N 次」", () => {
    const got = sum([
      tool("canvas_get_state"),
      tool("web_search"),
      tool("web_search"),
      tool("canvas_apply_ops"),
    ]);
    expect(got.phrase).toBe("读取了画布、搜索了网页 2 次、修改了画布");
  });

  test("超过 3 种动作：只写前 3 种，再加「等 N 项」（N 是动作种类数）", () => {
    const got = sum([
      tool("canvas_get_state"),
      tool("canvas_inspect_image"),
      tool("canvas_apply_ops"),
      tool("canvas_arrange"),
      tool("web_search"),
    ]);
    expect(got.phrase).toBe("读取了画布、查看了图片、修改了画布等 5 项");
  });

  test("不认识的动词：写成「调用了「动词」」，图标用通用的", () => {
    const got = sum([tool("new_tool", { verb: "冥想" })]);
    expect(got.phrase).toBe("调用了「冥想」");
    expect(got.icon).toBe("other");
  });

  test("看视频和截帧是两个动作，短语各自不同", () => {
    expect(sum([tool("canvas_inspect_video")]).phrase).toBe("查看了视频");
    expect(sum([tool("canvas_capture_frame")]).phrase).toBe("截取了视频帧");
  });
});

describe("toolIcon / formatSeconds", () => {
  test("每次调用的动作图标和摘要用同一张动词表", () => {
    expect(toolIcon({ name: "canvas_get_state" })).toBe("read");
    expect(toolIcon({ name: "x", verb: "截帧" })).toBe("frame");
    expect(toolIcon({ name: "x", verb: "冥想" })).toBe("other");
  });

  test("耗时：60 秒以内写秒，再长写分秒", () => {
    expect(formatSeconds(12)).toBe("12s");
    expect(formatSeconds(60)).toBe("1m 0s");
    expect(formatSeconds(72)).toBe("1m 12s");
  });
});
