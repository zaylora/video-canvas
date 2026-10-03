import { describe, expect, test } from "bun:test";

import type { ProbeResult } from "@/api/admin-storage/type";
import { probeSteps, probeSummary } from "@/utils/admin/storage-probe";

const ok = (index: number, name: string, ms = 40) => ({
  index,
  name,
  ok: true,
  skipped: false,
  duration_ms: ms,
});

describe("probeSteps：把探针结果整理成展示用的步骤", () => {
  test("全部通过：每步是 ok，带耗时", () => {
    const result: ProbeResult = {
      ok: true,
      steps: [ok(1, "鉴权与桶", 82), ok(2, "写入探针对象", 1200)],
    };
    expect(probeSteps(result)).toEqual([
      { index: 1, name: "鉴权与桶", status: "ok", durationText: "82ms" },
      { index: 2, name: "写入探针对象", status: "ok", durationText: "1.2s" },
    ]);
  });

  test("失败步骤带问题说明与原始错误，其后没执行的步骤标为 notrun", () => {
    const result: ProbeResult = {
      ok: false,
      steps: [
        ok(1, "鉴权与桶"),
        {
          index: 2,
          name: "写入探针对象",
          ok: false,
          skipped: false,
          duration_ms: 51,
          issue: { title: "密钥无权访问该桶", hint: "检查子账号权限", raw: "AccessDenied" },
        },
        { index: 3, name: "读取探针对象", ok: false, skipped: true, duration_ms: 0 },
      ],
    };
    const steps = probeSteps(result);
    expect(steps[1]).toEqual({
      index: 2,
      name: "写入探针对象",
      status: "failed",
      durationText: "51ms",
      issue: { title: "密钥无权访问该桶", hint: "检查子账号权限", raw: "AccessDenied" },
    });
    expect(steps[2]).toEqual({
      index: 3,
      name: "读取探针对象",
      status: "notrun",
      durationText: "",
    });
  });

  test("按配置主动跳过的步骤（ok 且 skipped）标为 skipped，不显示耗时", () => {
    const result: ProbeResult = {
      ok: true,
      steps: [{ index: 4, name: "签名地址可访问", ok: true, skipped: true, duration_ms: 0 }],
    };
    expect(probeSteps(result)[0]).toEqual({
      index: 4,
      name: "签名地址可访问",
      status: "skipped",
      durationText: "",
    });
  });

  test("失败但后端没给 issue 时，不带 issue 字段", () => {
    const step = probeSteps({
      ok: false,
      steps: [{ index: 1, name: "鉴权与桶", ok: false, skipped: false, duration_ms: 0 }],
    })[0];
    expect(step.status).toBe("failed");
    expect("issue" in step).toBe(false);
  });

  test("steps 为空或缺失时返回空数组", () => {
    expect(probeSteps({ ok: false, steps: [] })).toEqual([]);
    expect(probeSteps(null)).toEqual([]);
  });
});

describe("probeSummary", () => {
  test("全部通过", () => {
    expect(probeSummary({ ok: true, steps: [ok(1, "鉴权与桶")] })).toEqual({
      tone: "success",
      text: "全部通过",
    });
  });

  test("未通过：指出第几步失败", () => {
    expect(
      probeSummary({
        ok: false,
        steps: [
          ok(1, "鉴权与桶"),
          { index: 2, name: "写入探针对象", ok: false, skipped: false, duration_ms: 3 },
        ],
      }),
    ).toEqual({ tone: "danger", text: "第 2 步「写入探针对象」未通过" });
  });

  test("没有任何失败步骤但整体失败时给通用文案", () => {
    expect(probeSummary({ ok: false, steps: [] })).toEqual({ tone: "danger", text: "未通过" });
  });
});
