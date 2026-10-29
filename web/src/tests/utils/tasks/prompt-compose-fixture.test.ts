import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

import type { Capabilities } from "@/api/model/type";
import type { NodeKind } from "@/types";
import { buildTaskInput, resolveBindings, type IncomingLink } from "@/utils/tasks/capabilities";

/**
 * 与后端 internal/agent/canvasgraph 共用的 fixture：画布上点生成和 Agent 生成用同一套提示词合成规则，
 * 两边读同一份数据，任何一边改了规则而另一边没改，测试会立刻失败。
 */
const DIR = join(import.meta.dir, "../../../../../backend/internal/tests/testdata/canvasgraph");
const { cases } = JSON.parse(readFileSync(join(DIR, "prompt-compose.json"), "utf-8")) as {
  cases: {
    name: string;
    nodes: {
      id: string;
      kind: NodeKind;
      label: string;
      text?: string;
      assetId?: string;
      /** 角色卡的人物数据：补充图和声音的素材 id */
      character?: { name: string; extras?: string[]; voice?: string };
    }[];
    prompt: string;
    expect: string;
  }[];
};

const caps: Capabilities = {
  ops: ["omni"],
  refs: {
    image: { on: true, max: 9, max_mb: 10 },
    audio: { on: true, max: 3, max_mb: 15 },
    video: { on: true, max: 3, max_mb: 50 },
  },
  prompt: { max_length: 5000 },
  params: {},
};

describe("与后端共用的提示词合成规则", () => {
  for (const c of cases) {
    test(c.name, () => {
      const links: IncomingLink[] = c.nodes.map((node, i) => ({
        edgeId: `e${i}`,
        sourceId: node.id,
        sourceKind: node.kind,
        sourceLabel: node.label,
        targetHandle: null,
        assetId: node.assetId,
        text: node.text,
        character: node.character && {
          id: node.id,
          name: node.character.name,
          avatarAssetId: node.assetId ?? "",
          extraAssetIds: node.character.extras ?? [],
          voiceAssetId: node.character.voice ?? null,
        },
      }));
      const bindings = resolveBindings(caps, "omni", links);
      const built = buildTaskInput(caps, { prompt: c.prompt }, bindings, "omni", links);
      // 没有参考素材时全能参考本身会报素材错误，这里只关心提示词
      expect(built.errors.prompt).toBeUndefined();
      expect(built.input.prompt).toBe(c.expect);
    });
  }
});
