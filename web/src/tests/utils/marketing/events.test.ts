import { describe, expect, test } from "bun:test";

import type { Project, Round, Script } from "@/api/marketing/type";
import { isMarketingNode, marketingNodeId, scriptIdOfNode } from "@/utils/marketing/task-node";
import {
  mergeRound,
  mergeScript,
  roundTransition,
  scriptNotice,
  scriptTransition,
} from "@/utils/marketing/merge";
import { parseServerMessage } from "@/utils/ws/socket-client";

const script = (id: string, patch: Partial<Script> = {}): Script =>
  ({ id, roundId: "r1", status: "draft", revision: 1, shots: [], ...patch }) as Script;

const round = (id: string, no: number, patch: Partial<Round> = {}): Round =>
  ({ id, no, status: "ready", scripts: [], steps: [], ...patch }) as Round;

const project = (rounds: Round[]): Project =>
  ({ id: "p1", title: "晨雾", rounds, productAssets: [] }) as unknown as Project;

describe("mkt: 任务节点", () => {
  test("节点 id 与脚本 id 互转，只认 mkt: 前缀", () => {
    expect(marketingNodeId("x1")).toBe("mkt:x1");
    expect(isMarketingNode("mkt:x1")).toBe(true);
    expect(isMarketingNode("seg:x1")).toBe(false);
    expect(isMarketingNode("rec:1:0")).toBe(false);
    expect(scriptIdOfNode("mkt:x1")).toBe("x1");
    expect(scriptIdOfNode("seg:x1")).toBeNull();
  });
});

describe("并入实时数据", () => {
  test("轮次：已有的原地替换，新的按轮次号倒序插入，不改原对象", () => {
    const original = project([round("r2", 2), round("r1", 1)]);
    const replaced = mergeRound(original, round("r1", 1, { status: "failed" }));
    expect(replaced.rounds.map((r) => [r.id, r.status])).toEqual([
      ["r2", "ready"],
      ["r1", "failed"],
    ]);
    expect(original.rounds[1].status).toBe("ready");
    const inserted = mergeRound(original, round("r3", 3, { status: "scripting" }));
    expect(inserted.rounds.map((r) => r.id)).toEqual(["r3", "r2", "r1"]);
  });

  test("脚本：按 id 替换所在轮次里的那份；找不到轮次或脚本返回 null 让调用方重新拉", () => {
    const original = project([round("r1", 1, { scripts: [script("a"), script("b")] })]);
    const next = mergeScript(original, "r1", script("b", { status: "rendering" }));
    expect(next?.rounds[0].scripts.map((s) => [s.id, s.status])).toEqual([
      ["a", "draft"],
      ["b", "rendering"],
    ]);
    expect(mergeScript(original, "r9", script("b"))).toBeNull();
    expect(mergeScript(original, "r1", script("zz"))?.rounds[0].scripts).toHaveLength(3);
  });

  test("轮次事件里的旧快照不覆盖更新的脚本：轮次整体替换时脚本以事件为准", () => {
    const original = project([round("r1", 1, { scripts: [script("a")] })]);
    const next = mergeRound(original, round("r1", 1, { scripts: [script("a"), script("b")] }));
    expect(next.rounds[0].scripts).toHaveLength(2);
  });
});

describe("提醒时机", () => {
  test("写脚本结束：scripting 变 ready 或 failed 才算", () => {
    expect(roundTransition(round("r", 1, { status: "scripting" }), round("r", 1))).toBe("ready");
    expect(
      roundTransition(round("r", 1, { status: "scripting" }), round("r", 1, { status: "failed" })),
    ).toBe("failed");
    expect(roundTransition(round("r", 1), round("r", 1))).toBeNull();
    expect(
      roundTransition(
        round("r", 1, { status: "scripting" }),
        round("r", 1, { status: "canceled" }),
      ),
    ).toBeNull();
  });

  test("没见过的轮次直接是 ready：不提醒（可能是对账拉回来的旧数据）", () => {
    expect(roundTransition(undefined, round("r", 1))).toBeNull();
  });

  test("出片结束：rendering 变 done / failed", () => {
    expect(
      scriptTransition(script("a", { status: "rendering" }), script("a", { status: "done" })),
    ).toBe("done");
    expect(
      scriptTransition(script("a", { status: "rendering" }), script("a", { status: "failed" })),
    ).toBe("failed");
    expect(
      scriptTransition(script("a", { status: "draft" }), script("a", { status: "done" })),
    ).toBeNull();
    expect(scriptTransition(undefined, script("a", { status: "done" }))).toBeNull();
  });

  test("出片提醒：只在不看该项目时提醒成片出好，出片失败不弹提醒", () => {
    const rendering = script("a", { status: "rendering" });
    expect(scriptNotice(rendering, script("a", { status: "done" }), false)).toBe("done");
    expect(scriptNotice(rendering, script("a", { status: "done" }), true)).toBeNull();
    expect(scriptNotice(rendering, script("a", { status: "failed" }), false)).toBeNull();
    expect(scriptNotice(rendering, script("a", { status: "failed" }), true)).toBeNull();
  });
});

describe("WebSocket 消息识别", () => {
  test("marketing.round 要有 project_id 和 round.id", () => {
    const ok = JSON.stringify({
      type: "marketing.round",
      data: { project_id: "p1", round: { id: "r1" } },
    });
    expect(parseServerMessage(ok)?.type).toBe("marketing.round");
    expect(
      parseServerMessage(JSON.stringify({ type: "marketing.round", data: { project_id: "p1" } })),
    ).toBeNull();
  });

  test("marketing.script 要有 project_id、round_id 和 script.id", () => {
    const ok = JSON.stringify({
      type: "marketing.script",
      data: { project_id: "p1", round_id: "r1", script: { id: "x1" } },
    });
    expect(parseServerMessage(ok)?.type).toBe("marketing.script");
    expect(
      parseServerMessage(
        JSON.stringify({
          type: "marketing.script",
          data: { project_id: "p1", script: { id: "x1" } },
        }),
      ),
    ).toBeNull();
  });
});
