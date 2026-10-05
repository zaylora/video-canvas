import { describe, expect, test } from "bun:test";

import { planPreset, type PresetPick } from "@/utils/canvas/preset-rules";

const style = (id: string): PresetPick => ({ kind: "style", id });
const motion = (id: string): PresetPick => ({ kind: "motion", id });
const tpl = (id: string): PresetPick => ({ kind: "tpl", id });

describe("planPreset：从入口按钮选", () => {
  test("什么都没选：风格、运镜插在光标处，模板插在最前面", () => {
    expect(planPreset([], "style", "wuxia")).toEqual({ type: "insert", at: "cursor" });
    expect(planPreset([], "motion", "dolly_in")).toEqual({ type: "insert", at: "cursor" });
    expect(planPreset([], "tpl", "storyboard_25_grid")).toEqual({ type: "insert", at: "start" });
  });

  test("风格、运镜、模板各至多一个：再选同类的另一个，原位替换", () => {
    expect(planPreset([style("wuxia")], "style", "neon_punk")).toEqual({
      type: "replace",
      index: 0,
    });
    expect(planPreset([motion("tilt_up"), tpl("a")], "tpl", "b")).toEqual({
      type: "replace",
      index: 1,
    });
  });

  test("运镜可以同时有多个，各不相同；点已选的那条取消", () => {
    expect(planPreset([motion("pan_left")], "motion", "zoom_in")).toEqual({
      type: "insert",
      at: "cursor",
    });
    expect(planPreset([motion("pan_left"), motion("zoom_in")], "motion", "pan_left")).toEqual({
      type: "remove",
      index: 0,
    });
  });

  test("再点已选的那个：取消", () => {
    expect(planPreset([style("wuxia")], "style", "wuxia")).toEqual({ type: "remove", index: 0 });
    expect(planPreset([style("wuxia"), motion("zoom_in")], "motion", "zoom_in")).toEqual({
      type: "remove",
      index: 1,
    });
  });

  test("三类之间可以共存，互不影响", () => {
    expect(planPreset([motion("pan_left")], "style", "wuxia")).toEqual({
      type: "insert",
      at: "cursor",
    });
    expect(planPreset([style("wuxia")], "tpl", "a")).toEqual({ type: "insert", at: "start" });
    expect(planPreset([style("wuxia"), tpl("a")], "motion", "zoom_in")).toEqual({
      type: "insert",
      at: "cursor",
    });
  });
});

describe("planPreset：点提示词里的 chip 进来替换", () => {
  test("选另一个：替换被点的那个", () => {
    expect(planPreset([style("wuxia")], "style", "neon_punk", 0)).toEqual({
      type: "replace",
      index: 0,
    });
    expect(planPreset([style("wuxia"), motion("pan_left")], "motion", "crane_up", 1)).toEqual({
      type: "replace",
      index: 1,
    });
  });

  test("点自己：取消", () => {
    expect(planPreset([style("wuxia"), motion("pan_left")], "motion", "pan_left", 1)).toEqual({
      type: "remove",
      index: 1,
    });
  });

  test("多个运镜时只替换被点的那一个", () => {
    expect(planPreset([motion("pan_left"), motion("zoom_in")], "motion", "crane_up", 1)).toEqual({
      type: "replace",
      index: 1,
    });
  });

  test("运镜选了提示词别处已有的：不执行", () => {
    expect(planPreset([motion("pan_left"), motion("zoom_in")], "motion", "zoom_in", 0)).toEqual({
      type: "blocked",
    });
  });
});
