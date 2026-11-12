import { describe, expect, test } from "bun:test";

import type { Round, Script } from "@/api/marketing/type";
import {
  methodLabel,
  playableTakes,
  roundAspect,
  flagSummary,
  flaggedShots,
  isScriptBusy,
  narrationsChanged,
  renderAllState,
  scriptRenderState,
  scriptStatusLabel,
} from "@/utils/marketing/script";

const script = (patch: Partial<Script> = {}): Script => ({
  id: "x1",
  roundId: "r1",
  kind: "create",
  angle: "pain",
  title: "t",
  hook: "",
  shots: [
    {
      start: 0,
      end: 3,
      purpose: "",
      shot: "特写",
      camera: "固定",
      scene: "s",
      narration: "第一句",
      refShot: null,
    },
    {
      start: 3,
      end: 6,
      purpose: "",
      shot: "中景",
      camera: "固定",
      scene: "s",
      narration: "第二句",
      refShot: null,
    },
  ],
  music: "",
  voice: "",
  flags: [],
  status: "draft",
  edited: false,
  method: null,
  takes: [],
  renderCredits: 100,
  error: "",
  revision: 1,
  ...patch,
});

const round = (scripts: Script[], patch: Partial<Round> = {}): Round =>
  ({ id: "r1", status: "ready", scripts, ...patch }) as Round;

describe("脚本出片按钮", () => {
  test("正常脚本可出片；已出片 / 失败的叫重出", () => {
    expect(scriptRenderState(script(), 500)).toEqual({ disabled: false, reason: "", again: false });
    expect(scriptRenderState(script({ status: "done" }), 500).again).toBe(true);
    expect(scriptRenderState(script({ status: "failed" }), 500).again).toBe(true);
  });

  test("出片中 / 重写中禁用", () => {
    expect(scriptRenderState(script({ status: "rendering" }), 500).disabled).toBe(true);
    expect(scriptRenderState(script({ status: "rewriting" }), 500).disabled).toBe(true);
    expect(isScriptBusy("rendering")).toBe(true);
    expect(isScriptBusy("rewriting")).toBe(true);
    expect(isScriptBusy("done")).toBe(false);
  });

  test("旁白有问题：禁用并列出问题；比余额不足优先", () => {
    const flagged = script({
      flags: [
        { shot: 0, type: "forbidden", text: "最" },
        { shot: 1, type: "claim", text: "销量第一" },
        { shot: 1, type: "forbidden", text: "最" },
      ],
    });
    const state = scriptRenderState(flagged, 1);
    expect(state.disabled).toBe(true);
    expect(state.reason).toBe("旁白有问题：禁用说法「最」、背书无依据「销量第一」");
  });

  test("余额不足禁用；还没拿到余额不拦", () => {
    expect(scriptRenderState(script(), 50)).toMatchObject({ disabled: true, reason: "余额不足" });
    expect(scriptRenderState(script(), null).disabled).toBe(false);
  });

  test("问题镜头集合与按类型去重的问题标签", () => {
    const flags = [
      { shot: 0, type: "forbidden" as const, text: "最" },
      { shot: 2, type: "brand" as const, text: "某品牌" },
      { shot: 0, type: "forbidden" as const, text: "第一" },
    ];
    expect([...flaggedShots(flags)].sort()).toEqual([0, 2]);
    expect(flagSummary(flags)).toEqual(["forbidden", "brand"]);
  });

  test("状态标签", () => {
    expect(scriptStatusLabel("draft")).toBe("待出片");
    expect(scriptStatusLabel("rewriting")).toBe("重写中");
    expect(scriptStatusLabel("rendering")).toBe("出片中");
    expect(scriptStatusLabel("done")).toBe("已出片");
    expect(scriptStatusLabel("failed")).toBe("出片失败");
  });
});

describe("旁白编辑", () => {
  test("只有真的改了才需要保存：修剪首尾空白后比较", () => {
    const s = script();
    expect(narrationsChanged(s, ["第一句", "第二句"])).toBe(false);
    expect(narrationsChanged(s, [" 第一句 ", "第二句"])).toBe(false);
    expect(narrationsChanged(s, ["第一句", "改了"])).toBe(true);
    expect(narrationsChanged(s, ["第一句"])).toBe(true);
  });
});

describe("全部出片", () => {
  test("只统计可出片的脚本：出片中、重写中、旁白有问题的不算", () => {
    const state = renderAllState(
      round([
        script({ id: "a", renderCredits: 100 }),
        script({ id: "b", renderCredits: 100, status: "done" }),
        script({ id: "c", status: "rendering" }),
        script({ id: "d", flags: [{ shot: 0, type: "brand", text: "x" }] }),
      ]),
      1000,
    );
    expect(state).toEqual({ show: true, count: 2, credits: 200, disabled: false, reason: "" });
  });

  test("可出片的不足 2 份不显示按钮；轮次不是 ready 不显示；余额不足禁用写原因", () => {
    expect(renderAllState(round([script()]), 1000).show).toBe(false);
    expect(
      renderAllState(
        round([script({ id: "a" }), script({ id: "b" })], { status: "scripting" }),
        1000,
      ).show,
    ).toBe(false);
    const low = renderAllState(round([script({ id: "a" }), script({ id: "b" })]), 150);
    expect(low).toMatchObject({ show: true, disabled: true, reason: "余额不足" });
  });
});

describe("方法小字与画幅", () => {
  test("方法：内置 / 技能名 vN / 停用退回显示内置 / 没有方法也是内置", () => {
    const base = { skillTitle: "", skillName: "", version: 0, fallback: false };
    expect(methodLabel(null)).toBe("内置方法");
    expect(methodLabel({ ...base, source: "builtin" })).toBe("内置方法");
    expect(methodLabel({ ...base, source: "skill", skillName: "daihuo-fanpai", version: 2 })).toBe(
      "daihuo-fanpai v2",
    );
    expect(
      methodLabel({
        ...base,
        source: "skill",
        skillName: "daihuo-fanpai",
        version: 2,
        fallback: true,
      }),
    ).toBe("内置方法");
  });

  test("成片画幅取 aspect_ratio 或 ratio，不认识按 9:16", () => {
    expect(roundAspect({ params: { aspect_ratio: "16:9" } })).toEqual([16, 9]);
    expect(roundAspect({ params: { ratio: "3:4" } })).toEqual([3, 4]);
    expect(roundAspect({ params: { aspect_ratio: "auto" } })).toEqual([9, 16]);
    expect(roundAspect({ params: {} })).toEqual([9, 16]);
  });

  test("可播放的 take：只留有成片地址的", () => {
    const takes = [
      { id: "1", outputs: [] },
      { id: "2", outputs: [{ url: "u", posterUrl: "" }] },
    ] as unknown as Script["takes"];
    expect(playableTakes(script({ takes })).map((t) => t.id)).toEqual(["2"]);
  });
});
