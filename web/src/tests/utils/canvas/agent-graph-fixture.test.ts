import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

import type { Capabilities } from "@/api/model/type";
import { canConnectKinds } from "@/utils/canvas/canvas";
import { arrangeNodes, type ArrangeMode } from "@/utils/canvas/arrange";
import { nodeFrameSize, paramAspect, resolveNodeAspect } from "@/utils/canvas/node-frame";
import { createGroup, fitGroupToMembers, ungroup } from "@/utils/canvas/group";
import type { CanvasNode, FlowNode, NodeKind } from "@/types";

/**
 * 与后端 internal/agent/canvasgraph 共用的 fixture：Agent 在后端改画布，规则必须和前端手动操作一致，
 * 两边读同一份数据，任何一边改了规则而另一边没改，测试会立刻失败。
 */
const DIR = join(import.meta.dir, "../../../../../backend/internal/tests/testdata/canvasgraph");
const load = <T>(name: string): T => JSON.parse(readFileSync(join(DIR, name), "utf-8")) as T;

describe("与后端共用的连线规则", () => {
  const { cases } = load<{ cases: { from: NodeKind; to: NodeKind; ok: boolean }[] }>(
    "connect-rules.json",
  );
  for (const c of cases) {
    test(`${c.from} → ${c.to}：${c.ok ? "允许" : "拒绝"}`, () => {
      expect(canConnectKinds(c.from, "source", c.to)).toBe(c.ok);
    });
  }
});

type ArrangeCase = {
  name: string;
  layout: ArrangeMode;
  nodes: { id: string; kind: NodeKind; x: number; y: number }[];
  expected?: Record<string, { x: number; y: number }>;
};

/** 后端按种类使用的默认尺寸，前端测量后得到的就是这个值 */
const SIZE: Record<NodeKind, { width: number; height: number }> = {
  script: { width: 576, height: 324 },
  image: { width: 576, height: 324 },
  audio: { width: 576, height: 324 },
  video: { width: 576, height: 324 },
};

describe("与后端共用的排列算法", () => {
  const { cases } = load<{ cases: ArrangeCase[] }>("arrange-cases.json");
  for (const c of cases) {
    test(c.name, () => {
      const nodes: CanvasNode[] = c.nodes.map((n) => ({
        id: n.id,
        type: "canvas",
        position: { x: n.x, y: n.y },
        measured: SIZE[n.kind],
        data: { kind: n.kind, label: n.id },
      }));
      const out = arrangeNodes(nodes, c.layout);
      const got = Object.fromEntries([...out].map(([id, p]) => [id, { x: p.x, y: p.y }]));
      expect(got).toEqual(c.expected as Record<string, { x: number; y: number }>);
    });
  }
});

type GroupCase = {
  name: string;
  op: "group" | "ungroup" | "fit";
  groups?: { id: string; x: number; y: number; w: number; h: number }[];
  nodes: { id: string; kind: NodeKind; x: number; y: number; parent?: string }[];
  members?: string[];
  expected?: {
    group?: { x: number; y: number; w: number; h: number };
    nodes: Record<string, { x: number; y: number; parent: string | null }>;
  };
};

describe("与后端共用的打组 / 解组 / 贴合", () => {
  const { cases } = load<{ cases: GroupCase[] }>("group-cases.json");
  for (const c of cases) {
    test(c.name, () => {
      const groups: FlowNode[] = (c.groups ?? []).map((g) => ({
        id: g.id,
        type: "group",
        position: { x: g.x, y: g.y },
        width: g.w,
        height: g.h,
        data: { label: g.id },
      }));
      const members: FlowNode[] = c.nodes.map((n) => ({
        id: n.id,
        type: "canvas",
        position: { x: n.x, y: n.y },
        ...(n.parent ? { parentId: n.parent } : {}),
        measured: SIZE[n.kind],
        data: { kind: n.kind, label: n.id },
      }));
      const all = [...groups, ...members];
      let result: FlowNode[];
      if (c.op === "group") result = createGroup(all, c.members ?? [], "组", () => "g").nodes;
      else if (c.op === "ungroup") result = ungroup(all, "g");
      else result = fitGroupToMembers(all, "g");

      const grp = result.find((n) => n.type === "group");
      const got = {
        group: grp
          ? { x: grp.position.x, y: grp.position.y, w: grp.width, h: grp.height }
          : undefined,
        nodes: Object.fromEntries(
          result
            .filter((n) => n.type === "canvas")
            .map((n) => [n.id, { x: n.position.x, y: n.position.y, parent: n.parentId ?? null }]),
        ),
      };
      if (c.op === "ungroup") delete got.group;
      expect(got).toEqual(c.expected as typeof got);
    });
  }
});

type SizeCase = {
  name: string;
  kind: NodeKind | "director";
  params?: Record<string, unknown>;
  mediaAspect?: number;
  defaultAspect?: string;
  want: { w: number; h: number };
};

/** 前端画节点框时用的尺寸：和后端 canvasgraph.NodeSize 对同一份向量必须给出同样的宽高 */
function frontendSize(c: SizeCase): { width: number; height: number } {
  // 文本和导演台：NodeCard 默认 w-xl（576px）+ 预览区 16:9（NODE_PREVIEW_ASPECT）→ 324 高
  if (c.kind === "script" || c.kind === "director") return { width: 576, height: 324 };
  const params = c.params ?? {};
  // 模型的比例参数：节点里写了哪个就定义哪个；都没写才用「模型默认」
  const names = ["aspect_ratio", "ratio"].filter((k) => k in params);
  if (names.length === 0 && c.defaultAspect) names.push("aspect_ratio");
  const caps: Capabilities = {
    refs: { image: { max: 0 }, video: { max: 0 }, audio: { max: 0 } },
    prompt: { max_length: 100 },
    params: Object.fromEntries(
      names.map((n) => [
        n,
        { type: "enum", label: "比例", open: true, options: [], default: c.defaultAspect },
      ]),
    ),
  };
  const aspect = resolveNodeAspect({
    measured: null,
    declared: c.mediaAspect ?? null,
    selected: paramAspect(caps, params),
    mediaType: c.kind === "audio" ? "audio" : undefined,
  });
  return nodeFrameSize(aspect);
}

describe("与后端共用的节点尺寸规则", () => {
  const { cases } = load<{ cases: SizeCase[] }>("node-size-cases.json");
  for (const c of cases) {
    test(c.name, () => {
      const got = frontendSize(c);
      expect({ w: got.width, h: got.height }).toEqual(c.want);
    });
  }
});
