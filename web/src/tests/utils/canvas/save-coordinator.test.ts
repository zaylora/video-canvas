import { describe, expect, test } from "bun:test";

import type { CanvasGraphDto } from "@/api/canvas/type";
import { SaveCoordinator } from "@/utils/canvas/save-coordinator";
import type { SaveStatus } from "@/utils/canvas/save-status";

const graphOf = (n: number) =>
  ({
    nodes: [{ id: String(n) }],
    edges: [],
    viewport: { x: 0, y: 0, zoom: 1 },
  }) as unknown as CanvasGraphDto;

/** 微任务放一放，让 await 链走完 */
const settle = async () => {
  for (let i = 0; i < 20; i += 1) await Promise.resolve();
};

function setup({
  draft = true,
  initialVersion = 3,
}: { draft?: boolean; initialVersion?: number } = {}) {
  let now = 1_000_000;
  let seq = 0;
  const pending = new Map<number, { at: number; fn: () => void }>();
  const clock = {
    now: () => now,
    setTimer: (fn: () => void, ms: number) => {
      const id = ++seq;
      pending.set(id, { at: now + ms, fn });
      return id;
    },
    clearTimer: (id: unknown) => void pending.delete(id as number),
  };
  /** 往前走 ms，按时间顺序触发到期的定时器，每触发一个就把异步链走完 */
  async function advance(ms: number) {
    const target = now + ms;
    for (;;) {
      const next = [...pending.entries()]
        .filter(([, item]) => item.at <= target)
        .sort((a, b) => a[1].at - b[1].at)[0];
      if (!next) break;
      pending.delete(next[0]);
      now = Math.max(now, next[1].at);
      next[1].fn();
      await settle();
    }
    now = target;
    await settle();
  }

  let graphSeq = 0;
  let graphReady = true;
  const cloud = {
    calls: [] as { baseVersion: number; keepalive: boolean; graph: CanvasGraphDto }[],
    script: [] as ("ok" | "fail" | "conflict")[],
    version: initialVersion,
    hold: null as null | { release: () => void },
  };
  const drafts = { saves: [] as { base: number; graph: CanvasGraphDto }[], removes: 0, ok: true };
  const statuses: SaveStatus[] = [];
  let conflictEntered = 0;

  const coordinator = new SaveCoordinator({
    initialVersion,
    getGraph: () => (graphReady ? graphOf(++graphSeq) : null),
    saveCloud: async (args) => {
      cloud.calls.push(args);
      const action = cloud.script.shift() ?? "ok";
      if (cloud.hold === null && action === "ok") {
        cloud.version += 1;
        return { version: cloud.version };
      }
      if (action === "fail") throw new Error("network");
      if (action === "conflict") throw new Error("409");
      await new Promise<void>((resolve) => {
        cloud.hold = { release: resolve };
      });
      cloud.hold = null;
      cloud.version += 1;
      return { version: cloud.version };
    },
    isConflict: (error) => error instanceof Error && error.message === "409",
    onConflict: async () => {
      conflictEntered += 1;
      return true;
    },
    draft: draft
      ? {
          save: async (base, graph) => {
            drafts.saves.push({ base, graph });
            return drafts.ok;
          },
          remove: async () => {
            drafts.removes += 1;
          },
        }
      : null,
    onStatus: (status) => statuses.push(status),
    ...clock,
  });
  coordinator.setActive(true);
  return {
    coordinator,
    cloud,
    drafts,
    statuses,
    advance,
    settle,
    setGraphReady: (value: boolean) => {
      graphReady = value;
    },
    conflictEntered: () => conflictEntered,
  };
}

describe("本地草稿：停手 0.3 秒写入，最长 1 秒兜底", () => {
  test("改动后 0.3 秒写草稿，写的是基准版本", async () => {
    const t = setup();
    t.coordinator.changed();
    await t.advance(299);
    expect(t.drafts.saves.length).toBe(0);
    await t.advance(1);
    expect(t.drafts.saves.length).toBe(1);
    expect(t.drafts.saves[0].base).toBe(3);
  });

  test("每次新改动都重新计时（防抖），停手后只写一次", async () => {
    const t = setup();
    t.coordinator.changed();
    await t.advance(200);
    t.coordinator.changed();
    await t.advance(200);
    t.coordinator.changed();
    await t.advance(299);
    expect(t.drafts.saves.length).toBe(0);
    await t.advance(1);
    expect(t.drafts.saves.length).toBe(1);
  });

  test("连续编辑不会无限推迟：1 秒兜底必须写一次", async () => {
    const t = setup();
    for (let i = 0; i < 6; i += 1) {
      t.coordinator.changed();
      await t.advance(200);
    }
    expect(t.drafts.saves.length).toBeGreaterThanOrEqual(1);
  });

  test("写草稿不触发云端上传", async () => {
    const t = setup();
    t.coordinator.changed();
    await t.advance(500);
    expect(t.cloud.calls.length).toBe(0);
  });
});

describe("云端：停手 3 秒上传，最长 10 秒兜底", () => {
  test("停手 3 秒后上传，带基准版本", async () => {
    const t = setup();
    t.coordinator.changed();
    await t.advance(2999);
    expect(t.cloud.calls.length).toBe(0);
    await t.advance(1);
    expect(t.cloud.calls.length).toBe(1);
    expect(t.cloud.calls[0].baseVersion).toBe(3);
    expect(t.cloud.calls[0].keepalive).toBe(false);
  });

  test("连续编辑最长 10 秒必须上传一次", async () => {
    const t = setup();
    for (let i = 0; i < 12; i += 1) {
      t.coordinator.changed();
      await t.advance(1000);
    }
    expect(t.cloud.calls.length).toBeGreaterThanOrEqual(1);
    // 第一次上传发生在 10 秒兜底处，不会拖到 12 秒之后
    expect(t.cloud.calls.length).toBe(1);
  });

  test("上传成功且期间没有新改动：删掉草稿，版本更新", async () => {
    const t = setup();
    t.coordinator.changed();
    await t.advance(500);
    expect(t.drafts.saves.length).toBe(1);
    await t.advance(3000);
    expect(t.cloud.calls.length).toBe(1);
    expect(t.drafts.removes).toBe(1);
    expect(t.coordinator.version).toBe(4);
  });

  test("在途请求期间又有新改动：不并发发请求，落地后重写草稿（新基准）并按窗口补传", async () => {
    const t = setup();
    t.cloud.script.push("ok");
    t.cloud.hold = { release: () => {} };
    t.coordinator.changed();
    await t.advance(3000);
    expect(t.cloud.calls.length).toBe(1);
    t.coordinator.changed();
    await t.advance(4000);
    expect(t.cloud.calls.length).toBe(1);
    t.cloud.hold?.release();
    await t.settle();
    // 草稿没有被删，并且以新版本为基准重写了一次
    expect(t.drafts.removes).toBe(0);
    expect(t.drafts.saves.at(-1)?.base).toBe(t.coordinator.version);
    await t.advance(3000);
    expect(t.cloud.calls.length).toBe(2);
    expect(t.cloud.calls[1].baseVersion).toBe(t.coordinator.version - 1);
  });
});

describe("云端失败：延后变红、重试 3 次", () => {
  test("第一次失败不变红；5 秒后第一次重试仍失败才变红", async () => {
    const t = setup();
    t.cloud.script.push("fail", "fail");
    t.coordinator.changed();
    await t.advance(3000);
    expect(t.cloud.calls.length).toBe(1);
    expect(t.statuses).not.toContain("error");
    await t.advance(5000);
    expect(t.cloud.calls.length).toBe(2);
    expect(t.statuses.at(-1)).toBe("error");
  });

  test("瞬时抖动：第一次失败，重试成功，全程没有红色", async () => {
    const t = setup();
    t.cloud.script.push("fail");
    t.coordinator.changed();
    await t.advance(3000);
    await t.advance(5000);
    expect(t.cloud.calls.length).toBe(2);
    expect(t.statuses).not.toContain("error");
    expect(t.coordinator.version).toBe(4);
  });

  test("自动重试 3 次（5s、15s、30s）后停下，不再自动上传", async () => {
    const t = setup();
    t.cloud.script.push("fail", "fail", "fail", "fail", "fail", "fail");
    t.coordinator.changed();
    await t.advance(3000); // 第 1 次
    await t.advance(5000); // 重试 1
    await t.advance(15_000); // 重试 2
    await t.advance(30_000); // 重试 3
    expect(t.cloud.calls.length).toBe(4);
    await t.advance(120_000);
    expect(t.cloud.calls.length).toBe(4);
    expect(t.statuses.at(-1)).toBe("error");
  });

  test("红色保持到成功才恢复", async () => {
    const t = setup();
    t.cloud.script.push("fail", "fail");
    t.coordinator.changed();
    await t.advance(3000);
    await t.advance(5000);
    expect(t.statuses.at(-1)).toBe("error");
    await t.advance(15_000); // 第二次重试成功
    expect(t.statuses.at(-1)).not.toBe("error");
    expect(t.statuses.at(-1)).toBe("saved");
  });

  test("失败期间草稿一直在，内容不丢", async () => {
    const t = setup();
    t.cloud.script.push("fail", "fail", "fail", "fail");
    t.coordinator.changed();
    await t.advance(60_000);
    expect(t.drafts.saves.length).toBeGreaterThanOrEqual(1);
    expect(t.drafts.removes).toBe(0);
  });
});

describe("冲突（409）", () => {
  test("立即显示冲突，不自动重试", async () => {
    const t = setup();
    t.cloud.script.push("conflict");
    t.coordinator.changed();
    await t.advance(3000);
    expect(t.conflictEntered()).toBe(1);
    expect(t.statuses.at(-1)).toBe("conflict");
    await t.advance(120_000);
    expect(t.cloud.calls.length).toBe(1);
  });

  test("冲突期间本地仍继续写草稿，云端暂停", async () => {
    const t = setup();
    t.cloud.script.push("conflict");
    t.coordinator.changed();
    await t.advance(3000);
    const before = t.drafts.saves.length;
    t.coordinator.changed();
    await t.advance(20_000);
    expect(t.drafts.saves.length).toBeGreaterThan(before);
    expect(t.cloud.calls.length).toBe(1);
  });

  test("解除冲突后不再带着旧改动上传", async () => {
    const t = setup();
    t.cloud.script.push("conflict");
    t.coordinator.changed();
    await t.advance(3000);
    t.coordinator.dismissConflict();
    await t.advance(60_000);
    expect(t.cloud.calls.length).toBe(1);
    expect(t.coordinator.conflicted).toBe(false);
  });
});

describe("退出：强制保存", () => {
  test("flush：立即写草稿并立即上传，不等窗口", async () => {
    const t = setup();
    t.coordinator.changed();
    void t.coordinator.flush();
    await t.settle();
    expect(t.drafts.saves.length).toBe(1);
    expect(t.cloud.calls.length).toBe(1);
  });

  test("flush 的草稿写入在同步阶段就发起：页面销毁前来得及", () => {
    const t = setup();
    t.coordinator.changed();
    void t.coordinator.flush();
    // 没有任何 await：同步部分已经把草稿写入交出去了
    expect(t.drafts.saves.length).toBe(1);
  });

  test("keepalive 透传给云端请求", async () => {
    const t = setup();
    t.coordinator.changed();
    await t.coordinator.flush({ keepalive: true });
    expect(t.cloud.calls[0].keepalive).toBe(true);
  });

  test("没有改动：flush 什么都不做，返回已保存", async () => {
    const t = setup();
    expect(await t.coordinator.flush()).toBe(true);
    expect(t.drafts.saves.length).toBe(0);
    expect(t.cloud.calls.length).toBe(0);
  });

  test("云端失败时 flush 返回 false，但草稿已经写好", async () => {
    const t = setup();
    t.cloud.script.push("fail");
    t.coordinator.changed();
    expect(await t.coordinator.flush()).toBe(false);
    expect(t.drafts.saves.length).toBe(1);
  });

  test("图谱还没准备好：不写草稿也不崩", async () => {
    const t = setup();
    t.setGraphReady(false);
    t.coordinator.changed();
    await t.advance(5000);
    expect(t.drafts.saves.length).toBe(0);
    expect(t.cloud.calls.length).toBe(0);
  });
});

describe("状态栏防抖", () => {
  test("改动后立即变成正在保存", async () => {
    const t = setup();
    t.coordinator.changed();
    expect(t.statuses.at(-1)).toBe("saving");
  });

  test("连续编辑期间不闪回已保存", async () => {
    const t = setup();
    for (let i = 0; i < 8; i += 1) {
      t.coordinator.changed();
      await t.advance(400);
    }
    expect(t.statuses).not.toContain("saved");
  });

  test("停下后静默 0.8 秒才变成已保存", async () => {
    const t = setup();
    t.coordinator.changed();
    await t.advance(300); // 草稿写完
    expect(t.statuses.at(-1)).toBe("saving");
    await t.advance(799);
    expect(t.statuses.at(-1)).toBe("saving");
    await t.advance(1);
    expect(t.statuses.at(-1)).toBe("saved");
  });

  test("云端上传本身不改变状态栏（静默）", async () => {
    const t = setup();
    t.coordinator.changed();
    await t.advance(1200);
    expect(t.statuses.at(-1)).toBe("saved");
    const count = t.statuses.length;
    await t.advance(3000); // 云端上传发生在这里
    expect(t.cloud.calls.length).toBe(1);
    expect(t.statuses.slice(count)).not.toContain("saving");
  });

  test("不活跃（组件卸载）后不再通知状态", async () => {
    const t = setup();
    t.coordinator.changed();
    t.coordinator.setActive(false);
    const count = t.statuses.length;
    await t.advance(10_000);
    expect(t.statuses.length).toBe(count);
  });
});

describe("没有本地草稿能力时的降级", () => {
  test("没有草稿（如无法识别用户）：云端窗口不变，仍按 3 秒上传", async () => {
    const t = setup({ draft: false });
    t.coordinator.changed();
    await t.advance(3000);
    expect(t.cloud.calls.length).toBe(1);
  });

  test("没有草稿：状态栏在云端未同步期间显示正在保存", async () => {
    const t = setup({ draft: false });
    t.coordinator.changed();
    await t.advance(1500);
    expect(t.statuses.at(-1)).toBe("saving");
    await t.advance(3000);
    expect(t.statuses.at(-1)).toBe("saved");
  });

  test("草稿写入失败：降级为只靠云端，不抛错", async () => {
    const t = setup();
    t.drafts.ok = false;
    t.coordinator.changed();
    await t.advance(300);
    expect(t.drafts.saves.length).toBe(1);
    t.coordinator.changed();
    await t.advance(1500);
    // 草稿已判定不可用：后续不再反复尝试写草稿
    expect(t.drafts.saves.length).toBe(1);
    await t.advance(3000);
    expect(t.cloud.calls.length).toBe(1);
  });
});

describe("与改名共用版本号", () => {
  test("exclusive 排在在途上传后面执行", async () => {
    const t = setup();
    t.cloud.script.push("ok");
    t.cloud.hold = { release: () => {} };
    t.coordinator.changed();
    await t.advance(3000);
    const order: string[] = [];
    const renamed = t.coordinator.exclusive(async () => {
      order.push("rename");
    });
    await t.settle();
    expect(order).toEqual([]);
    t.cloud.hold?.release();
    await renamed;
    expect(order).toEqual(["rename"]);
  });

  test("acceptVersion：改名成功后把新版本接过来，后面的上传用它作基准", async () => {
    const t = setup();
    t.coordinator.acceptVersion(9);
    expect(t.coordinator.version).toBe(9);
    t.coordinator.changed();
    await t.advance(3000);
    expect(t.cloud.calls[0].baseVersion).toBe(9);
  });
});
