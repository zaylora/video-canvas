import { existsSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, test } from "bun:test";

import {
  MOTION_CATEGORIES,
  MOTION_PRESETS,
  STYLE_CATEGORIES,
  STYLE_PRESETS,
  TEMPLATE_GROUPS,
  TEMPLATE_PRESETS,
  findPreset,
} from "@/constants/presets";

/** 预设图片在 public/presets 下，路径里 presets/ 之后的部分就是相对路径 */
const publicFile = (url: string) =>
  join(import.meta.dir, "../../../public/presets", url.split("presets/")[1]);

describe("预设目录", () => {
  test("三类预设的条数和 id 都不重复", () => {
    expect(STYLE_PRESETS).toHaveLength(45);
    expect(MOTION_PRESETS).toHaveLength(33);
    expect(TEMPLATE_PRESETS).toHaveLength(9);
    for (const list of [STYLE_PRESETS, MOTION_PRESETS, TEMPLATE_PRESETS]) {
      expect(new Set(list.map((item) => item.id)).size).toBe(list.length);
    }
  });

  test("每一条都有名字和能展开的提示词", () => {
    for (const item of [...STYLE_PRESETS, ...MOTION_PRESETS, ...TEMPLATE_PRESETS]) {
      expect(item.name.trim()).not.toBe("");
      expect(item.prompt.trim().length).toBeGreaterThan(10);
    }
  });

  test("分类和分组都在声明过的列表里，每个分类至少有一条", () => {
    const styleCats = new Set(STYLE_CATEGORIES.map((item) => item.id));
    const motionCats = new Set(MOTION_CATEGORIES.map((item) => item.id));
    for (const item of STYLE_PRESETS) expect(styleCats.has(item.category)).toBe(true);
    for (const item of MOTION_PRESETS) expect(motionCats.has(item.category)).toBe(true);
    for (const cat of STYLE_CATEGORIES)
      expect(STYLE_PRESETS.some((item) => item.category === cat.id)).toBe(true);
    for (const cat of MOTION_CATEGORIES)
      expect(MOTION_PRESETS.some((item) => item.category === cat.id)).toBe(true);
    for (const group of TEMPLATE_GROUPS)
      expect(TEMPLATE_PRESETS.some((item) => item.group === group)).toBe(true);
    for (const item of TEMPLATE_PRESETS) expect(TEMPLATE_GROUPS).toContain(item.group);
  });

  test("风格封面和运镜示意图的文件都真实存在", () => {
    for (const item of [...STYLE_PRESETS, ...MOTION_PRESETS])
      expect(existsSync(publicFile(item.cover))).toBe(true);
  });

  test("按种类和 id 查找，找不到返回 undefined", () => {
    expect(findPreset("motion", "descending_orbit")?.name).toBe("盘旋下降");
    expect(findPreset("style", "wuxia")?.name).toBe("武侠江湖");
    expect(findPreset("tpl", "multi_camera_nine_grid")?.name).toBe("多机位九宫格");
    expect(findPreset("style", "descending_orbit")).toBeUndefined();
    expect(findPreset("tpl", "nope")).toBeUndefined();
  });
});
