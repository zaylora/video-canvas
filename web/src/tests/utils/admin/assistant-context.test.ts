import { afterEach, beforeEach, describe, expect, test } from "bun:test";

import type { AgentApprovalDto, AgentEventDto } from "@/api/agent/type";
import {
  ADMIN_CONFIG_CHANGED,
  adminConfigChangeTargets,
  notifyAdminConfigChanged,
} from "@/utils/admin/config-changed";
import {
  buildPageContext,
  pageContextChips,
  describePageContext,
} from "@/utils/admin/assistant/page-context";

describe("页面上下文", () => {
  test("在渠道页：带路由和选中的渠道、模型", () => {
    expect(
      buildPageContext(
        "/admin/ai/channels",
        { channelKey: "kling", modelKey: "kling-v2-1" },
        new Set(),
      ),
    ).toEqual({ route: "/admin/ai/channels", channel_key: "kling", model_key: "kling-v2-1" });
  });

  test("没有选中项时只有路由；空串当作没有", () => {
    expect(buildPageContext("/admin/settings/pricing", {}, new Set())).toEqual({
      route: "/admin/settings/pricing",
    });
    expect(buildPageContext("/admin/ai/channels", { modelKey: "" }, new Set())).toEqual({
      route: "/admin/ai/channels",
    });
  });

  test("引用标签：页面名在前，然后是渠道、模型、插件", () => {
    const chips = pageContextChips("/admin/ai/channels", {
      modelKey: "m1",
      channelKey: "c1",
      pluginKey: "p1",
    });
    expect(chips.map((c) => [c.id, c.value])).toEqual([
      ["page", "/admin/ai/channels"],
      ["channel", "c1"],
      ["model", "m1"],
      ["plugin", "p1"],
    ]);
    expect(chips.map((c) => c.label)).toEqual(["模型渠道", "渠道：c1", "模型：m1", "插件：p1"]);
  });

  test("用户移除的标签不再进上下文；全部移除就不带上下文", () => {
    const hints = { channelKey: "c1", modelKey: "m1" };
    expect(buildPageContext("/admin/ai/channels", hints, new Set(["model"]))).toEqual({
      route: "/admin/ai/channels",
      channel_key: "c1",
    });
    expect(buildPageContext("/admin/ai/channels", hints, new Set(["page", "channel"]))).toEqual({
      route: "/admin/ai/channels",
      model_key: "m1",
    });
    expect(
      buildPageContext("/admin/ai/channels", hints, new Set(["page", "channel", "model"])),
    ).toBeUndefined();
  });

  test("不认识的路径用「后台」作页面名", () => {
    expect(pageContextChips("/admin/unknown", {})[0].label).toBe("后台");
  });

  test("已发出消息下方的引用：从上下文还原标签", () => {
    expect(
      describePageContext({ route: "/admin/ai/channels", model_key: "m1" }).map((c) => c.label),
    ).toEqual(["模型渠道", "模型：m1"]);
    expect(describePageContext(undefined)).toEqual([]);
  });
});

const approvalEvent = (
  data: Partial<AgentApprovalDto>,
  type = "approval.decided",
): AgentEventDto => ({
  session_id: "s1",
  canvas_id: "0",
  run_id: "r1",
  seq: 9,
  type,
  data: {
    id: "ap1",
    run_id: "r1",
    kind: "admin_change",
    status: "executed",
    payload: {
      order_no: "CO-1",
      title: "t",
      reason: "r",
      items: [
        { target: "model", key: "a" },
        { target: "pricing_settings", key: "pricing" },
        { target: "model", key: "b" },
      ],
    },
    quote_credits: 0,
    decision: null,
    result: {
      items: [
        { index: 0, status: "applied" },
        { index: 1, status: "failed" },
        { index: 2, status: "applied" },
      ],
      applied_by: "zhang",
      applied_at: "2026-11-02T06:00:00Z",
      audit_ids: [1, 2],
    },
    expires_at: "",
    ...data,
  } as unknown as Record<string, unknown>,
  created_at: "2026-11-02T06:00:00Z",
});

describe("admin-config-changed", () => {
  test("用户和系统设置操作成功后刷新对应页面", () => {
    const targets = ["user", "register_settings", "smtp_settings", "storage", "image_processor"];
    const ev = approvalEvent({});
    ev.data.payload = { items: targets.map((target) => ({ target, key: "1" })) };
    ev.data.result = { items: targets.map((_, index) => ({ index, status: "applied" })) };
    expect(adminConfigChangeTargets(ev)).toEqual(targets);
  });
  test("管理操作失败后重新读取部分生效的用户状态或检查结果", () => {
    const ev = approvalEvent({});
    ev.data.payload = {
      items: [
        { target: "storage", key: "1" },
        { target: "user", key: "2" },
      ],
    };
    ev.data.result = {
      items: [
        { index: 0, status: "failed" },
        { index: 1, status: "failed" },
      ],
    };
    expect(adminConfigChangeTargets(ev)).toEqual(["storage", "user"]);
  });
  test("插件、渠道和新建模型分别刷新对应配置列表", () => {
    const ev = approvalEvent({});
    const p = ev.data.payload as { items: unknown[] };
    p.items = ["plugin", "channel", "model_create"].map((target) => ({ target, key: target }));
    ev.data.result = { items: [0, 1, 2].map((index) => ({ index, status: "applied" })) };
    expect(adminConfigChangeTargets(ev)).toEqual(["plugin", "channel", "model"]);
  });
  test("approval.decided 里有已应用的条目：列出受影响的对象，去重", () => {
    expect(adminConfigChangeTargets(approvalEvent({}))).toEqual(["model"]);
  });

  test("定价设置条目已应用也会列出", () => {
    const ev = approvalEvent({
      result: {
        items: [{ index: 1, status: "applied" }],
        applied_by: "z",
        applied_at: "",
        audit_ids: [],
      } as never,
    });
    expect(adminConfigChangeTargets(ev)).toEqual(["pricing_settings"]);
  });

  test("没有已应用的条目、不是 admin_change、不是 approval.decided、结果为空：都不触发", () => {
    const none = {
      items: [{ index: 0, status: "failed" }],
      applied_by: "z",
      applied_at: "",
      audit_ids: [],
    } as never;
    expect(adminConfigChangeTargets(approvalEvent({ result: none }))).toBeNull();
    expect(adminConfigChangeTargets(approvalEvent({ kind: "generate" }))).toBeNull();
    expect(adminConfigChangeTargets(approvalEvent({}, "approval.created"))).toBeNull();
    expect(adminConfigChangeTargets(approvalEvent({ result: null }))).toBeNull();
  });

  describe("派发 window 事件", () => {
    const g = globalThis as unknown as { window?: EventTarget };
    let saved: EventTarget | undefined;
    beforeEach(() => {
      saved = g.window;
      g.window = new EventTarget();
    });
    afterEach(() => {
      g.window = saved;
    });

    test("detail.targets 带上受影响的对象", () => {
      const got: unknown[] = [];
      g.window!.addEventListener(ADMIN_CONFIG_CHANGED, (e) => got.push((e as CustomEvent).detail));
      notifyAdminConfigChanged(approvalEvent({}));
      expect(got).toEqual([{ targets: ["model"] }]);
    });

    test("不满足条件不派发", () => {
      let n = 0;
      g.window!.addEventListener(ADMIN_CONFIG_CHANGED, () => n++);
      notifyAdminConfigChanged(approvalEvent({ kind: "generate" }));
      expect(n).toBe(0);
    });
  });
});
