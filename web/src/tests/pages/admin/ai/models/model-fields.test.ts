import { describe, expect, test } from "bun:test";

import type { ChannelView } from "@/api/admin/ai/type";
import { fieldOfPath, modelChecks } from "@/pages/admin/ai/models/model-fields";
import { resolveModelChannel } from "@/utils/admin/model-channel";

describe("fieldOfPath：校验问题定位到字段", () => {
  test("协议问题定位到“能力与协议”区块，渠道问题仍定位到所属渠道", () => {
    expect(fieldOfPath("channels[0].protocol")).toEqual({ tab: "basic", id: "model-protocol" });
    expect(fieldOfPath("channels[0].protocol.settings.region")).toEqual({
      tab: "basic",
      id: "model-protocol",
    });
    expect(fieldOfPath("channels[0].channel")).toEqual({ tab: "basic", id: "model-channel" });
    expect(fieldOfPath("channels[0].upstream_model")).toEqual({
      tab: "basic",
      id: "model-upstream",
    });
    expect(fieldOfPath("channels")).toEqual({ tab: "basic", id: "model-channel" });
  });

  test("其他顶层字段按页签分", () => {
    expect(fieldOfPath("kind")?.tab).toBe("basic");
    expect(fieldOfPath("capabilities.params.a")?.tab).toBe("params");
    expect(fieldOfPath("pricing.unit")?.tab).toBe("price");
    expect(fieldOfPath("nope")).toBeNull();
  });
});

describe("modelChecks：启用前待办", () => {
  const channel = {
    key: "main",
    name: "主渠道",
    enabled: true,
    secret_set: true,
  } as ChannelView;

  test("待配置的模型：没选能力、没选协议都列出（协议问题挂在 channels[0].protocol）", () => {
    const body = {
      key: "m",
      label: "L",
      kind: "",
      channels: [{ channel: "main", upstream_model: "u" }],
    };
    const checks = modelChecks(body, resolveModelChannel(body, [channel], []), []);
    expect(checks.map((item) => item.path)).toEqual(["kind"]);
    const text = { ...body, kind: "text" };
    const textChecks = modelChecks(text, resolveModelChannel(text, [channel], []), []);
    expect(textChecks.find((item) => item.path === "channels[0].protocol")?.text).toBe(
      "该模型还没有选择调用协议",
    );
  });

  test("清单没加载好时不对渠道与协议下结论", () => {
    const body = {
      key: "m",
      label: "L",
      kind: "text",
      channels: [{ channel: "x", upstream_model: "u" }],
    };
    const checks = modelChecks(body, resolveModelChannel(body, [], []), [], false);
    expect(checks).toEqual([]);
  });

  test("后端返回的问题按路径归到页签", () => {
    const body = {
      key: "m",
      label: "L",
      kind: "agent",
      channels: [{ channel: "main", upstream_model: "u" }],
    };
    const checks = modelChecks(body, resolveModelChannel(body, [channel], []), [
      { path: "channels[0].protocol", message: "Agent 模型不需要选择协议" },
      { path: "pricing.unit", message: "价格不对" },
    ]);
    expect(checks.map((item) => item.tab)).toEqual(["basic", "price"]);
  });
});
