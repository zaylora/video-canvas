import { describe, expect, test } from "bun:test";

import type { CanvasNodeData, NodeStoryboard, StoryboardShot } from "@/types";
import {
  composeCandidates,
  composeCandidatesFrom,
  missingVideoSeqsFrom,
  mmss,
  seqText,
} from "@/utils/storyboard/compose";

/** 造一镜：只有 id 和可选的视频节点关联，其余字段与合成无关 */
const shot = (id: string, videoNodeId?: string): StoryboardShot =>
  ({ id, videoNodeId }) as StoryboardShot;

const board = (...shots: StoryboardShot[]): NodeStoryboard => ({ shots }) as NodeStoryboard;

/** 有历次产物的视频节点：activeOutputId 指向第二版 */
const withOutputs = (assetIds: string[], activeIndex: number): CanvasNodeData =>
  ({
    kind: "video",
    label: "视频",
    outputs: assetIds.map((assetId) => ({ id: assetId, src: `/files/${assetId}`, assetId })),
    activeOutputId: assetIds[activeIndex],
    src: `/files/${assetIds[activeIndex]}`,
    assetId: assetIds[activeIndex],
  }) as CanvasNodeData;

describe("合成候选：行序就是成片顺序", () => {
  test("按分镜表行序取当前采用的版本，顺序与行序一致", () => {
    const b = board(shot("s1", "v1"), shot("s2", "v2"), shot("s3", "v3"));
    const got = composeCandidatesFrom(b, [
      withOutputs(["a1", "a1b"], 1),
      withOutputs(["a2"], 0),
      withOutputs(["a3"], 0),
    ]);
    expect(got.map((item) => item.assetId)).toEqual(["a1b", "a2", "a3"]);
    expect(got.map((item) => item.seq)).toEqual([0, 1, 2]);
  });

  test("没有关联视频节点的行、还没出片的行被跳过，seq 仍指向真实行号", () => {
    const b = board(shot("s1", "v1"), shot("s2"), shot("s3", "v3"));
    const got = composeCandidatesFrom(b, [withOutputs(["a1"], 0), undefined, undefined]);
    expect(got).toEqual([{ seq: 0, assetId: "a1" }]);
  });

  test("旧画布节点只有 src + assetId、没有 outputs：必须兜底认出来，不能静默漏镜", () => {
    const legacy = {
      kind: "video",
      label: "视频",
      src: "/files/legacy",
      assetId: "legacy",
      mediaType: "video",
    } as CanvasNodeData;
    const got = composeCandidatesFrom(board(shot("s1", "v1")), [legacy]);
    expect(got).toEqual([{ seq: 0, assetId: "legacy" }]);
  });

  test("关联的不是视频节点（比如图片）不参与合成", () => {
    const image = { kind: "image", label: "图", src: "/files/i", assetId: "i" } as CanvasNodeData;
    expect(composeCandidatesFrom(board(shot("s1", "n1")), [image])).toEqual([]);
  });

  test("候选为空时缺失镜头是全部行号", () => {
    const b = board(shot("s1"), shot("s2"));
    expect(missingVideoSeqsFrom(b, [undefined, undefined])).toEqual([0, 1]);
  });

  test("按 id 取的版本与按行数据取的版本结果一致", () => {
    const b = board(shot("s1", "v1"), shot("s2", "v2"));
    const datas = [withOutputs(["a1"], 0), withOutputs(["a2"], 0)];
    const byId = composeCandidates(b, (nodeId) =>
      nodeId === "v1" ? datas[0] : nodeId === "v2" ? datas[1] : undefined,
    );
    expect(byId).toEqual(composeCandidatesFrom(b, datas));
  });
});

describe("文案", () => {
  test("镜号从 1 起补两位", () => {
    expect(seqText([0, 2])).toBe("01、03");
  });

  test("时长按 mm:ss", () => {
    expect(mmss(42_000)).toBe("00:42");
    expect(mmss(125_000)).toBe("02:05");
  });
});
