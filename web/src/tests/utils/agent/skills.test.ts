import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import type { AxiosAdapter, InternalAxiosRequestConfig } from "axios";

import { getAgentSkills } from "@/api/agent/skill";
import instance from "@/utils/requests/request";
import { filterSkills, type AgentSkillOption } from "@/utils/agent/skills";

const originalAdapter = instance.defaults.adapter;

/** bun 测试环境没有 DOM，请求拦截器读 token 要用到 localStorage，用内存实现顶替 */
beforeEach(() => {
  (globalThis as { localStorage?: Storage }).localStorage = {
    getItem: () => null,
    setItem: () => {},
    removeItem: () => {},
    clear: () => {},
    key: () => null,
    length: 0,
  } as Storage;
});

afterEach(() => {
  instance.defaults.adapter = originalAdapter;
});

/** 让真实的请求实例直接返回给定的后端响应体，并记下发出的请求 */
function stubBackend(body: unknown) {
  const requests: InternalAxiosRequestConfig[] = [];
  const adapter: AxiosAdapter = async (config) => {
    requests.push(config);
    return { data: body, status: 200, statusText: "OK", headers: {}, config };
  };
  instance.defaults.adapter = adapter;
  return requests;
}

describe("@ 弹层的技能目录：接口契约", () => {
  test("读 GET /agent/skills，原样给出 name、title、description、source", async () => {
    const requests = stubBackend({
      code: 0,
      msg: "ok",
      data: [
        {
          name: "script-breakdown",
          title: "剧本拆镜",
          description: "按场次拆分",
          source: "builtin",
        },
        { name: "dialogue-polish", title: "对白润色", description: "改对白", source: "imported" },
      ],
    });
    const list = await getAgentSkills();
    expect(requests).toHaveLength(1);
    expect(requests[0].method).toBe("get");
    expect(requests[0].url).toBe("/agent/skills");
    expect(list).toEqual([
      { name: "script-breakdown", title: "剧本拆镜", description: "按场次拆分", source: "builtin" },
      { name: "dialogue-polish", title: "对白润色", description: "改对白", source: "imported" },
    ]);
  });

  test("后端返回 null（没有启用的技能）时给空数组", async () => {
    stubBackend({ code: 0, msg: "ok", data: null });
    expect(await getAgentSkills()).toEqual([]);
  });
});

describe("filterSkills", () => {
  const skills: AgentSkillOption[] = [
    {
      name: "script-breakdown",
      title: "剧本拆镜",
      description: "按场次和镜头拆分",
      source: "builtin",
    },
    {
      name: "keyframe-prompt",
      title: "关键帧提示词",
      description: "含景别、构图、光线",
      source: "builtin",
    },
    {
      name: "dialogue-polish",
      title: "对白润色",
      description: "改对白，注意光线",
      source: "imported",
    },
  ];

  test("按名称、技能名、说明搜索，忽略大小写；没有命中为空；空关键词返回全部", () => {
    expect(filterSkills(skills, "拆镜").map((s) => s.name)).toEqual(["script-breakdown"]);
    expect(filterSkills(skills, "KEYFRAME").map((s) => s.name)).toEqual(["keyframe-prompt"]);
    expect(filterSkills(skills, "光线").map((s) => s.name)).toEqual([
      "keyframe-prompt",
      "dialogue-polish",
    ]);
    expect(filterSkills(skills, "不存在的东西")).toEqual([]);
    expect(filterSkills(skills, "  ")).toHaveLength(3);
  });
});
