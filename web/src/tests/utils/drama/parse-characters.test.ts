import { describe, expect, test } from "bun:test";

import { cardDisplay, parseCharacters } from "@/utils/drama/parse-characters";

describe("人物传记 Markdown 拆成人物卡片", () => {
  test("截图里的写法：分组 → 人名列表项 → 子项「**标签**：内容」", () => {
    const md = [
      "### 主要人物",
      "",
      "- **林阳（外号：壕天帝）**",
      "  - **身份**：蓝星大学班长，穿越后拜入青元剑宗。",
      "  - **动机**：利用物价贬值十亿倍系统极致氪金。",
      "  - **能力/根基**：身怀八品九系灵根。",
      "- **小黑**",
      "  - **身份**：林阳的灵宠。",
    ].join("\n");
    expect(parseCharacters(md)).toEqual([
      {
        group: "主要人物",
        name: "林阳",
        alias: "外号：壕天帝",
        fields: [
          { label: "身份", value: "蓝星大学班长，穿越后拜入青元剑宗。" },
          { label: "动机", value: "利用物价贬值十亿倍系统极致氪金。" },
          { label: "能力/根基", value: "身怀八品九系灵根。" },
        ],
        notes: [],
      },
      {
        group: "主要人物",
        name: "小黑",
        alias: "",
        fields: [{ label: "身份", value: "林阳的灵宠。" }],
        notes: [],
      },
    ]);
  });

  test("多个分组，分组也可以是单独一行的粗体", () => {
    const md = [
      "**主要人物**",
      "- 林阳",
      "  - 身份：班长",
      "**次要人物**",
      "- 唐雨",
      "  - 身份：师姐",
    ].join("\n");
    const cards = parseCharacters(md);
    expect(cards.map((card) => [card.group, card.name])).toEqual([
      ["主要人物", "林阳"],
      ["次要人物", "唐雨"],
    ]);
    expect(cards[1].fields).toEqual([{ label: "身份", value: "师姐" }]);
  });

  test("一个人物一个标题，下面跟字段", () => {
    const md = [
      "## 林阳",
      "- **身份**：班长",
      "**动机**：变强",
      "## 方清瑶",
      "- 身份：青梅竹马",
    ].join("\n");
    const cards = parseCharacters(md);
    expect(cards.map((card) => card.name)).toEqual(["林阳", "方清瑶"]);
    expect(cards[0].group).toBe("");
    expect(cards[0].fields).toEqual([
      { label: "身份", value: "班长" },
      { label: "动机", value: "变强" },
    ]);
  });

  test("一行一个人：「**人名**：介绍」，介绍放进说明", () => {
    const md = ["## 人物", "- **林阳**：蓝星大学班长", "- **小黑（Hei）**：灵宠"].join("\n");
    const cards = parseCharacters(md);
    expect(cards).toEqual([
      { group: "人物", name: "林阳", alias: "", fields: [], notes: ["蓝星大学班长"] },
      { group: "人物", name: "小黑", alias: "Hei", fields: [], notes: ["灵宠"] },
    ]);
  });

  test("不认识的标签照样显示，没标签的行放进说明，一句都不丢", () => {
    const md = [
      "- **林阳**",
      "  - **口头禅**：氪就完事",
      "  - 性格冲动但重情义",
      "  - **角色弧线**：从隐藏实力到以凡人模样示人",
    ].join("\n");
    const [card] = parseCharacters(md);
    expect(card.fields).toEqual([
      { label: "口头禅", value: "氪就完事" },
      { label: "角色弧线", value: "从隐藏实力到以凡人模样示人" },
    ]);
    expect(card.notes).toEqual(["性格冲动但重情义"]);
  });

  test("内容里的粗体、斜体记号去掉，只留文字", () => {
    const md = ["- **林阳**", "  - **能力**：持有法宝**山河欺天镜**与*青剑*"].join("\n");
    expect(parseCharacters(md)[0].fields[0].value).toBe("持有法宝山河欺天镜与青剑");
  });

  test("空串、只有空白：没有人物", () => {
    expect(parseCharacters("")).toEqual([]);
    expect(parseCharacters("  \n \n")).toEqual([]);
  });

  test("不是人物列表的文字（如「当前文本未发现明确人物」）：没有人物，交给原文显示", () => {
    expect(parseCharacters("当前文本未发现明确人物。")).toEqual([]);
    expect(parseCharacters("# 人物传记\n\n这是一段介绍文字，没有列表。")).toEqual([]);
  });
});

describe("卡片上怎么显示", () => {
  const card = parseCharacters(
    [
      "- **小熊**",
      "  - **正式名称**：小熊",
      "  - **别称**：邻居、老伴",
      "  - **身份**：55岁，小丽的丈夫",
    ].join("\n"),
  )[0];

  test("「正式名称」和名字一样时不重复显示，别称挪到名字下面", () => {
    expect(cardDisplay(card)).toEqual({
      name: "小熊",
      subtitle: "邻居、老伴",
      fields: [{ label: "身份", value: "55岁，小丽的丈夫" }],
    });
  });

  test("名字括号里的外号和「别称」都放进名字下面", () => {
    const [withAlias] = parseCharacters("- **林阳（外号：壕天帝）**\n  - **别名**：林哥");
    expect(cardDisplay(withAlias).subtitle).toBe("外号：壕天帝 · 林哥");
    expect(cardDisplay(withAlias).fields).toEqual([]);
  });

  test("「正式名称」和名字不一样时照常作为一项资料显示", () => {
    const [other] = parseCharacters("- **小丽**\n  - **正式名称**：王丽");
    expect(cardDisplay(other).fields).toEqual([{ label: "正式名称", value: "王丽" }]);
  });
});
