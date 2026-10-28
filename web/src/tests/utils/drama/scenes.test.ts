import { describe, expect, test } from "bun:test";

import { extractScenes } from "@/utils/drama/scenes";

describe("剧本场次识别（目录用）", () => {
  test("二到四级标题都算场次，一级标题是集名不算", () => {
    const md =
      "# 第 3 集 怀表\n\n## 3-1 雾港码头 夜 外\n动作\n\n### 3-2 晚声旧书店\n\n#### 3-3 码头";
    expect(extractScenes(md)).toEqual(["3-1 雾港码头 夜 外", "3-2 晚声旧书店", "3-3 码头"]);
  });

  test("整行加粗且像场次编号的行也算（模型常这么写）", () => {
    const md = "**1-1 雾港码头 夜 外**\n林晚奔跑。\n\n**第二场 书店 内**\n\n**林晚**\n台词";
    expect(extractScenes(md)).toEqual(["1-1 雾港码头 夜 外", "第二场 书店 内"]);
  });

  test("去掉标题里的加粗、行内代码和结尾的井号，多余空白压成一个", () => {
    expect(extractScenes("## **场景 1**  `码头`   夜 ##")).toEqual(["场景 1 码头 夜"]);
  });

  test("加粗的角色名、普通正文不会被当成场次", () => {
    expect(extractScenes("**林晚**\n（喘气）\n信上说三点。\n\n**沈既白**")).toEqual([]);
  });

  test("没有场次、空文本：空数组", () => {
    expect(extractScenes("")).toEqual([]);
    expect(extractScenes("只有一段话。")).toEqual([]);
  });

  test("代码围栏里的井号不算标题", () => {
    expect(extractScenes("```\n## 不是场次\n```\n## 真场次")).toEqual(["真场次"]);
  });
});
