import { describe, expect, test } from "bun:test";

import { AGENT_SKILLS, filterSkills } from "@/constants/agent-skills";

describe("内置技能清单", () => {
  test("与后端内置的五个技能同名", () => {
    expect(AGENT_SKILLS.map((s) => s.name).sort()).toEqual([
      "character-turnaround",
      "keyframe-prompt",
      "scene-setting",
      "script-breakdown",
      "video-motion-prompt",
    ]);
  });

  test("按名称、技能名、说明搜索；没有命中为空；空关键词全部", () => {
    expect(filterSkills("拆镜").map((s) => s.name)).toEqual(["script-breakdown"]);
    expect(filterSkills("KEYFRAME").map((s) => s.name)).toEqual(["keyframe-prompt"]);
    expect(filterSkills("光线").length).toBeGreaterThan(1);
    expect(filterSkills("不存在的东西")).toEqual([]);
    expect(filterSkills("  ")).toHaveLength(5);
  });
});
