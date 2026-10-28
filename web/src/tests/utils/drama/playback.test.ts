import { describe, expect, test } from "bun:test";

import type { Segment } from "@/api/drama/type";
import {
  buildPlaylist,
  clipOffsets,
  firstIndexFrom,
  locate,
  nextIndex,
  playheadX,
  timeAt,
} from "@/utils/drama/playback";
import { clipWidth } from "@/utils/drama/segments";

const seg = (
  id: string,
  seq: number,
  seconds: number,
  url?: string,
  durationMs?: number,
): Segment => ({
  id,
  episodeNo: 1,
  seq,
  seconds,
  prompt: "",
  shots: [],
  keyframe: null,
  takes: url
    ? [
        {
          taskId: `t${id}`,
          status: "succeeded",
          asset: {
            id,
            url,
            kind: "video",
            mimeType: "video/mp4",
            byteSize: 1,
            width: null,
            height: null,
            durationMs: durationMs ?? null,
            fileName: null,
          },
          errorMessage: "",
          createdAt: "x",
        },
      ]
    : [],
  chosenTaskId: url ? `t${id}` : "",
  status: "idle",
  revision: 1,
  updatedAt: "x",
});

const list = [
  seg("a", 0, 10, "https://cdn/a.mp4"),
  seg("b", 1, 6), // 没有采用视频：跳过
  seg("c", 2, 8, "https://cdn/c.mp4", 7500), // 实际时长 7.5 秒
  seg("d", 3, 4, "https://cdn/d.mp4"),
];

describe("连播列表 buildPlaylist", () => {
  test("按 seq 排序，没有采用视频的片段跳过", () => {
    const playlist = buildPlaylist([list[3], list[1], list[0], list[2]]);
    expect(playlist.map((item) => item.id)).toEqual(["a", "c", "d"]);
  });

  test("时长优先用素材的实际时长，没有就用片段秒数；开始 / 结束时间连续累加", () => {
    const playlist = buildPlaylist(list);
    expect(playlist.map((item) => [item.start, item.end])).toEqual([
      [0, 10],
      [10, 17.5],
      [17.5, 21.5],
    ]);
  });

  test("没有任何可播放片段是空列表", () => {
    expect(buildPlaylist([seg("x", 0, 5)])).toEqual([]);
  });
});

describe("时间与片段换算", () => {
  const playlist = buildPlaylist(list);

  test("locate：总时间落在哪一段、段内偏移多少", () => {
    expect(locate(playlist, 0)).toEqual({ index: 0, offset: 0 });
    expect(locate(playlist, 9.5)).toEqual({ index: 0, offset: 9.5 });
    expect(locate(playlist, 10)).toEqual({ index: 1, offset: 0 });
    expect(locate(playlist, 20)).toEqual({ index: 2, offset: 2.5 });
  });

  test("locate：越界取两端；空列表是 null", () => {
    expect(locate(playlist, -3)).toEqual({ index: 0, offset: 0 });
    expect(locate(playlist, 99)).toEqual({ index: 2, offset: 4 });
    expect(locate([], 1)).toBeNull();
  });

  test("timeAt 是 locate 的反函数，段内偏移不超过该段时长", () => {
    expect(timeAt(playlist, 1, 2)).toBe(12);
    expect(timeAt(playlist, 1, 99)).toBe(17.5);
    expect(timeAt(playlist, 9, 0)).toBe(0);
  });

  test("nextIndex：有下一段给下标，最后一段后是 null", () => {
    expect(nextIndex(playlist, 0)).toBe(1);
    expect(nextIndex(playlist, 2)).toBeNull();
  });

  test("firstIndexFrom：点到没有视频的片段，跳到它之后第一个能播的；之后没有为 -1", () => {
    expect(firstIndexFrom(playlist, list[0])).toBe(0);
    expect(firstIndexFrom(playlist, list[1])).toBe(1);
    expect(firstIndexFrom(playlist, list[3])).toBe(2);
    expect(firstIndexFrom(buildPlaylist([list[0]]), list[3])).toBe(-1);
  });
});

describe("播放头位置（片段条上的像素）", () => {
  test("clipOffsets：每个片段起点 = 前面各片段宽度 + 间距（含没有视频的）", () => {
    const offsets = clipOffsets(list);
    expect(offsets[0]).toBe(0);
    expect(offsets[1]).toBe(clipWidth(10) + 4);
    expect(offsets[2]).toBe(clipWidth(10) + 4 + clipWidth(6) + 4);
  });

  test("playheadX：片段起点 + 进度 × 该片段宽度；进度夹在 0–1", () => {
    const base = clipOffsets(list)[2];
    expect(playheadX(list, "c", 0)).toBe(base);
    expect(playheadX(list, "c", 0.5)).toBe(base + clipWidth(8) / 2);
    expect(playheadX(list, "c", 3)).toBe(base + clipWidth(8));
    expect(playheadX(list, "none", 0.5)).toBe(0);
  });
});
