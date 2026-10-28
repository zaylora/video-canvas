import { describe, expect, test } from "bun:test";

import type { Segment, SegmentTake } from "@/api/drama/type";
import {
  applyOrder,
  batchTargets,
  chooseCheck,
  chosenTake,
  clipWidth,
  latestError,
  mergeSegment,
  orderIds,
  playableTake,
  removeSegmentFrom,
  sameOrder,
  totalSeconds,
} from "@/utils/drama/segments";

const take = (taskId: string, status: SegmentTake["status"], url?: string): SegmentTake => ({
  taskId,
  status,
  asset: url
    ? {
        id: taskId,
        url,
        kind: "video",
        mimeType: "video/mp4",
        byteSize: 1,
        width: null,
        height: null,
        durationMs: null,
        fileName: null,
      }
    : null,
  errorMessage: status === "failed" ? "审核未通过" : "",
  createdAt: "2026-10-28T10:00:00Z",
});

const seg = (id: string, seq: number, patch: Partial<Segment> = {}): Segment => ({
  id,
  episodeNo: 1,
  seq,
  seconds: 10,
  prompt: "提示词",
  shots: [],
  keyframe: null,
  takes: [],
  chosenTaskId: "",
  status: "idle",
  revision: 1,
  updatedAt: "x",
  ...patch,
});

describe("片段条重排（契约 §10 PUT order）", () => {
  const list = [seg("a", 0), seg("b", 1), seg("c", 2)];

  test("applyOrder 按 ids 排好并重写 seq（从 0 起连续）", () => {
    const next = applyOrder(list, ["c", "a", "b"]);
    expect(next.map((s) => s.id)).toEqual(["c", "a", "b"]);
    expect(next.map((s) => s.seq)).toEqual([0, 1, 2]);
  });

  test("ids 里没有的片段排在最后，不丢；ids 里多出来的忽略", () => {
    expect(applyOrder(list, ["b", "zzz"]).map((s) => s.id)).toEqual(["b", "a", "c"]);
  });

  test("orderIds / sameOrder：拖完没变就不用请求", () => {
    expect(orderIds(list)).toEqual(["a", "b", "c"]);
    expect(sameOrder(["a", "b"], ["a", "b"])).toBe(true);
    expect(sameOrder(["a", "b"], ["b", "a"])).toBe(false);
    expect(sameOrder(["a"], ["a", "b"])).toBe(false);
  });
});

describe("并入服务端推送的片段（WebSocket drama.segment）", () => {
  const list = [seg("a", 0), seg("b", 1)];

  test("同 id 更新；revision 更小的迟到快照被忽略，相同 revision 视为 take 状态变化而采用", () => {
    const newer = mergeSegment(list, seg("a", 0, { revision: 3, prompt: "新" }));
    expect(newer[0].prompt).toBe("新");
    const stale = mergeSegment(newer, seg("a", 0, { revision: 2, prompt: "旧" }));
    expect(stale[0].prompt).toBe("新");
    const sameRev = mergeSegment(newer, seg("a", 0, { revision: 3, status: "running" }));
    expect(sameRev[0].status).toBe("running");
  });

  test("没见过的 id 插入，并按 seq 排序", () => {
    const next = mergeSegment([seg("a", 0), seg("b", 2)], seg("x", 1, { revision: 1 }));
    expect(next.map((s) => s.id)).toEqual(["a", "x", "b"]);
  });

  test("removeSegmentFrom 删除后 seq 不重排（以服务端为准）", () => {
    expect(removeSegmentFrom(list, "a").map((s) => s.id)).toEqual(["b"]);
  });
});

describe("take 采用逻辑（契约 §9.2）", () => {
  const s = seg("a", 0, {
    takes: [take("1", "succeeded", "u1"), take("2", "failed"), take("3", "running")],
    chosenTaskId: "1",
    status: "done",
  });

  test("chosenTake / playableTake 取采用的那条；没有采用或采用的没有素材就不能播", () => {
    expect(chosenTake(s)?.taskId).toBe("1");
    expect(playableTake(s)?.asset?.url).toBe("u1");
    expect(playableTake(seg("b", 1))).toBeNull();
    expect(playableTake({ ...s, chosenTaskId: "2" })).toBeNull();
  });

  test("只能采用本片段已成功的 take；已经是采用的不重复请求", () => {
    const multi = { ...s, takes: [...s.takes, take("4", "succeeded", "u4")] };
    expect(chooseCheck(multi, "4")).toBe("ok");
    expect(chooseCheck(multi, "1")).toBe("already");
    expect(chooseCheck(multi, "2")).toBe("invalid");
    expect(chooseCheck(multi, "3")).toBe("invalid");
    expect(chooseCheck(multi, "nope")).toBe("invalid");
  });

  test("成功但没有素材地址的 take 不能采用", () => {
    const broken = seg("a", 0, { takes: [take("9", "succeeded")] });
    expect(chooseCheck(broken, "9")).toBe("invalid");
  });

  test("latestError：只在 error 状态下给最近一条失败原因", () => {
    const failed = seg("a", 0, {
      status: "error",
      takes: [take("1", "failed"), take("2", "failed")],
    });
    expect(latestError(failed)).toBe("审核未通过");
    expect(latestError(seg("a", 0, { status: "done" }))).toBeNull();
    expect(latestError(seg("a", 0, { status: "error", takes: [] }))).toBe("生成失败");
  });
});

describe("批量生成：对没有采用视频的片段逐个提交", () => {
  test("跳过已有采用视频的、生成中的、没有提示词的", () => {
    const list = [
      seg("done", 0, {
        takes: [take("1", "succeeded", "u")],
        chosenTaskId: "1",
        status: "done",
      }),
      seg("running", 1, { status: "running" }),
      seg("empty", 2, { prompt: "  " }),
      seg("idle", 3),
      seg("err", 4, { status: "error", takes: [take("2", "failed")] }),
    ];
    expect(batchTargets(list).map((s) => s.id)).toEqual(["idle", "err"]);
  });
});

describe("片段条尺寸与时长", () => {
  test("宽度按时长，太短的有下限", () => {
    expect(clipWidth(10)).toBe(120);
    expect(clipWidth(2)).toBe(64);
  });

  test("总时长是各片段之和", () => {
    expect(totalSeconds([seg("a", 0, { seconds: 8 }), seg("b", 1, { seconds: 12 })])).toBe(20);
    expect(totalSeconds([])).toBe(0);
  });
});
