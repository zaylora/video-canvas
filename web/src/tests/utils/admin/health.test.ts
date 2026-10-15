import { describe, expect, test } from "bun:test";

import type { ModelStatus } from "@/api/admin/ai/type";
import { HEALTH_UI_TONE, MODEL_STATUS_LABEL, MODEL_STATUS_TONE } from "@/utils/admin/health";

/** 后端 service.ModelHealthOf 会给出的全部状态：前端只做展示映射，不重算状态 */
const ALL_STATUS: ModelStatus[] = ["online", "broken", "offline", "pending"];

describe("模型状态的标签与色调", () => {
  test("每种状态都有中文标签", () => {
    expect(MODEL_STATUS_LABEL).toEqual({
      online: "在线",
      broken: "不可用",
      offline: "未上线",
      pending: "待配置",
    });
  });

  test("待配置是 warning 语气（提醒运营还要选能力 / 协议），不是危险色", () => {
    expect(MODEL_STATUS_TONE.pending).toBe("warn");
    expect(HEALTH_UI_TONE[MODEL_STATUS_TONE.pending]).toBe("warning");
  });

  test("其余状态：在线绿、不可用红、未上线灰", () => {
    expect(HEALTH_UI_TONE[MODEL_STATUS_TONE.online]).toBe("success");
    expect(HEALTH_UI_TONE[MODEL_STATUS_TONE.broken]).toBe("danger");
    expect(HEALTH_UI_TONE[MODEL_STATUS_TONE.offline]).toBe("neutral");
  });

  test("标签与色调覆盖后端的全部状态，不会漏映射", () => {
    for (const status of ALL_STATUS) {
      expect(MODEL_STATUS_LABEL[status]).toBeTruthy();
      expect(HEALTH_UI_TONE[MODEL_STATUS_TONE[status]]).toBeTruthy();
    }
  });
});

describe("渠道健康色调到 UI 语气", () => {
  test("ok / warn / off 各对应一种语气；bad 后端已不再给出，映射保留以防旧数据", () => {
    expect(HEALTH_UI_TONE).toEqual({
      ok: "success",
      warn: "warning",
      off: "neutral",
      bad: "danger",
    });
  });
});
