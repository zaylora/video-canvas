import { describe, expect, test } from "bun:test";

import type { CanvasNodeData } from "@/types";
import { composeInputOf, type ComposeSource } from "@/utils/canvas/compose-input";

/** 老节点：只有 src + assetId，没有 outputs（旧画布就是这样存的） */
const legacyVideo = (label: string, assetId: string): CanvasNodeData => ({
  kind: "video",
  label,
  status: "done",
  src: `/files/${assetId}.mp4`,
  mediaType: "video",
  assetId,
});

/** 生成了多版：activeOutputId 指哪版就取哪版 */
const multiVersion = (label: string): CanvasNodeData => ({
  kind: "video",
  label,
  status: "done",
  src: "/files/2.mp4",
  mediaType: "video",
  assetId: "2",
  activeOutputId: "2",
  outputs: [
    { id: "1", src: "/files/1.mp4", mediaType: "video", assetId: "1", createdAt: 1 },
    { id: "2", src: "/files/2.mp4", mediaType: "video", assetId: "2", createdAt: 2 },
  ],
});

/** 还在生成：没有素材 */
const pendingVideo = (label: string): CanvasNodeData => ({
  kind: "video",
  label,
  status: "running",
});

const source = (
  id: string,
  data: CanvasNodeData | undefined,
  y: number,
  x: number,
): ComposeSource => ({
  id,
  data,
  y,
  x,
});

const ids = (input: ReturnType<typeof composeInputOf>) =>
  input.candidates.map((item) => item.assetId);

describe("成片节点的输入收集", () => {
  test("只收视频：图片、音频、文本混在入边里一律不进成片", () => {
    const input = composeInputOf([
      source("v1", legacyVideo("镜一", "11"), 0, 0),
      source("i1", { kind: "image", label: "图", assetId: "21", src: "/files/21.png" }, 0, 300),
      source("a1", { kind: "audio", label: "音", assetId: "31", src: "/files/31.mp3" }, 0, 600),
      source("s1", { kind: "script", label: "文", text: "旁白" }, 0, 900),
    ]);
    expect(input.total).toBe(1);
    expect(ids(input)).toEqual(["11"]);
    expect(input.missing).toEqual([]);
  });

  test("顺序按画布位置：先上下、再左右，与连线先后无关", () => {
    const input = composeInputOf([
      source("c", legacyVideo("第三", "33"), 900, 0),
      source("a", legacyVideo("第一", "11"), 0, 120),
      source("b", legacyVideo("第二", "22"), 0, 600),
    ]);
    expect(ids(input)).toEqual(["11", "22", "33"]);
    expect(input.rows.map((row) => row.label)).toEqual(["第一", "第二", "第三"]);
    expect(input.rows.map((row) => row.seq)).toEqual([0, 1, 2]);
  });

  test("老节点没有 outputs 也能取到素材：走 activeOutputIdOf 兜底，不静默漏段", () => {
    const input = composeInputOf([source("v1", legacyVideo("老画布", "77"), 0, 0)]);
    expect(ids(input)).toEqual(["77"]);
    expect(input.rows[0].assetId).toBe("77");
  });

  test("多版本节点取当前选中的那一版", () => {
    const input = composeInputOf([source("v1", multiVersion("选过版本"), 0, 0)]);
    expect(ids(input)).toEqual(["2"]);
  });

  test("还在生成的段进 missing，段号按位置占住，不挤掉后面的", () => {
    const input = composeInputOf([
      source("v1", legacyVideo("一", "11"), 0, 0),
      source("v2", pendingVideo("二"), 0, 600),
      source("v3", legacyVideo("三", "33"), 0, 1200),
    ]);
    expect(input.total).toBe(3);
    expect(ids(input)).toEqual(["11", "33"]);
    expect(input.missing).toEqual([1]);
    expect(input.rows.map((row) => row.id)).toEqual(["v1", "v2", "v3"]);
  });

  test("节点已被删掉（取不到数据）时不计入，也不崩", () => {
    const input = composeInputOf([
      source("gone", undefined, 0, 0),
      source("v1", legacyVideo("还在", "11"), 0, 600),
    ]);
    expect(input.total).toBe(1);
    expect(ids(input)).toEqual(["11"]);
  });

  test("没有上游时是空的：界面据此显示空状态，不发请求", () => {
    const input = composeInputOf([]);
    expect(input).toEqual({ rows: [], candidates: [], missing: [], total: 0 });
  });

  test("候选的 seq 与行号一致：后端靠它说「第几段」", () => {
    const input = composeInputOf([
      source("v1", legacyVideo("一", "11"), 0, 0),
      source("v2", pendingVideo("二"), 0, 600),
      source("v3", legacyVideo("三", "33"), 0, 1200),
    ]);
    expect(input.candidates).toEqual([
      { seq: 0, assetId: "11" },
      { seq: 2, assetId: "33" },
    ]);
  });
});

describe("成片节点的手动调序", () => {
  const three = [
    source("v1", legacyVideo("一", "11"), 0, 0),
    source("v2", legacyVideo("二", "22"), 0, 600),
    source("v3", legacyVideo("三", "33"), 0, 1200),
  ];

  test("传了 composeOrder 就以它为准，位置不再决定顺序", () => {
    const input = composeInputOf(three, ["v3", "v1", "v2"]);
    expect(ids(input)).toEqual(["33", "11", "22"]);
    expect(input.rows.map((row) => row.seq)).toEqual([0, 1, 2]);
  });

  test("顺序里没列到的上游接在末尾，仍按位置排：新连进来的视频不用重排", () => {
    const input = composeInputOf(three, ["v3"]);
    expect(ids(input)).toEqual(["33", "11", "22"]);
  });

  test("顺序里留着已删节点的 id 时忽略它，不影响其余顺序", () => {
    const input = composeInputOf(three, ["gone", "v2", "v1"]);
    expect(ids(input)).toEqual(["22", "11", "33"]);
  });

  test("空顺序等价于纯按位置：读档后 composeOrder 被清掉也照旧", () => {
    expect(ids(composeInputOf(three, []))).toEqual(ids(composeInputOf(three)));
  });
});
