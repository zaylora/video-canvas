import { describe, expect, test } from "bun:test";

import { pendingTileTitles } from "@/utils/canvas/history-pending";
import { makeTask } from "../tasks/fixtures";

const NOW = Date.parse("2026-09-29T10:00:00Z");

describe("pendingTileTitles：历史记录里占位格的标题", () => {
  test("还没提交给上游的任务显示“排队中”，已经在生成的显示“生成中”", () => {
    const tasks = {
      "1": makeTask({ id: 1, status: "running" }),
      "2": makeTask({ id: 2, status: "pending" }),
      "3": makeTask({ id: 3, status: "pending", submitted_at: "2026-09-29T09:59:50Z" }),
      "4": makeTask({ id: 4, status: "queued" }),
    };
    expect(pendingTileTitles([1, 2, 3, 4], tasks, NOW)).toEqual([
      "生成中",
      "排队中",
      "生成中",
      "生成中",
    ]);
  });

  test("转存中显示“即将完成”", () => {
    const tasks = { "1": makeTask({ id: 1, status: "finalizing" }) };
    expect(pendingTileTitles(["1"], tasks, NOW)).toEqual(["即将完成"]);
  });

  test("任务快照还没到时按“生成中”显示，不猜排队", () => {
    expect(pendingTileTitles([9], {}, NOW)).toEqual(["生成中"]);
  });

  test("没有任务 id 时给定数量的占位都按“生成中”", () => {
    expect(pendingTileTitles([], {}, NOW, 2)).toEqual(["生成中", "生成中"]);
  });
});
