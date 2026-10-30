import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { CharacterCards } from "@/pages/drama/workspace/character-cards";
import { parseCharacters } from "@/utils/drama/parse-characters";

const md = [
  "### 主要人物",
  "- **林阳（外号：壕天帝）**",
  "  - **身份**：蓝星大学班长",
  "  - **动机**：极致氪金",
  "- **小黑**",
  "  - 灵宠，贪吃",
  "### 反派",
  "- **赵坤**",
  "  - **身份**：外门执事",
].join("\n");

describe("人物传记卡片", () => {
  const out = renderToStaticMarkup(<CharacterCards cards={parseCharacters(md)} />);

  test("每个人物一张卡片，模样统一：同一个头像图标", () => {
    expect(out.split('data-slot="character-card"').length - 1).toBe(3);
    expect(out.split("lucide-user-round").length - 1).toBe(3);
  });

  test("名字、外号、资料标签和内容都显示出来", () => {
    for (const text of [
      "林阳",
      "外号：壕天帝",
      "身份",
      "蓝星大学班长",
      "动机",
      "极致氪金",
      "灵宠，贪吃",
    ]) {
      expect(out).toContain(text);
    }
  });

  test("固定两列铺满内容区，人少的分组不在右边留空列", () => {
    expect(out).toContain("md:grid-cols-2");
    expect(out).not.toContain("auto-fill");
  });

  test("分组名标在组上方，按原文顺序", () => {
    expect(out.indexOf("主要人物")).toBeGreaterThan(-1);
    expect(out.indexOf("主要人物")).toBeLessThan(out.indexOf("反派"));
    expect(out.indexOf("反派")).toBeLessThan(out.indexOf("赵坤"));
  });
});
