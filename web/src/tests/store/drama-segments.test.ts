import { beforeEach, describe, expect, spyOn, test } from "bun:test";

import type { BackendSegmentView } from "@/api/drama/type";
import { useDramaSegmentsStore, segmentKey } from "@/store/drama-segments";
import { handleDramaSegmentEvent, reconcileWatchedSegments } from "@/utils/drama/segment-events";
import service from "@/utils/requests/service";
import { parseServerMessage } from "@/utils/ws/socket-client";

const raw = (patch: Partial<BackendSegmentView> = {}): BackendSegmentView => ({
  id: "s1",
  episode_no: 1,
  seq: 0,
  seconds: 10,
  prompt: "p",
  shots: null,
  keyframe: null,
  takes: null,
  chosen_task_id: "",
  status: "idle",
  revision: 1,
  updated_at: "x",
  ...patch,
});

beforeEach(() => {
  useDramaSegmentsStore.setState({ byEpisode: {}, watching: {} });
});

describe("片段 store", () => {
  test("setList 整体替换，apply 并入单条，remove 删除", () => {
    const store = useDramaSegmentsStore.getState();
    store.setList("d1", 1, [{ ...handleShape("a", 0) }, { ...handleShape("b", 1) }]);
    expect(useDramaSegmentsStore.getState().byEpisode[segmentKey("d1", 1)]).toHaveLength(2);
    store.apply("d1", 1, { ...handleShape("a", 0), prompt: "新", revision: 2 });
    expect(useDramaSegmentsStore.getState().byEpisode[segmentKey("d1", 1)]![0].prompt).toBe("新");
    store.remove("d1", 1, "b");
    expect(
      useDramaSegmentsStore.getState().byEpisode[segmentKey("d1", 1)]!.map((s) => s.id),
    ).toEqual(["a"]);
  });

  test("watch 引用计数：多处打开同一集，全部关掉才不再对账", () => {
    const store = useDramaSegmentsStore.getState();
    const off1 = store.watch("d1", 1);
    const off2 = store.watch("d1", 1);
    off1();
    expect(useDramaSegmentsStore.getState().watching[segmentKey("d1", 1)]).toBe(1);
    off2();
    expect(useDramaSegmentsStore.getState().watching[segmentKey("d1", 1)]).toBeUndefined();
  });
});

describe("WebSocket drama.segment", () => {
  test("消息解析：合法的通过，缺字段的丢弃", () => {
    const ok = parseServerMessage(
      JSON.stringify({
        type: "drama.segment",
        data: { drama_id: "d1", episode_no: 1, segment: raw() },
      }),
    );
    expect(ok?.type).toBe("drama.segment");
    expect(
      parseServerMessage(
        JSON.stringify({ type: "drama.segment", data: { drama_id: "d1", episode_no: 1 } }),
      ),
    ).toBeNull();
    expect(
      parseServerMessage(
        JSON.stringify({
          type: "drama.segment",
          data: { drama_id: 1, episode_no: 1, segment: raw() },
        }),
      ),
    ).toBeNull();
  });

  test("事件处理：映射后并进对应短剧的对应集", () => {
    handleDramaSegmentEvent({
      drama_id: "d9",
      episode_no: 2,
      segment: raw({ episode_no: 2, status: "running", prompt: "来自推送" }),
    });
    const list = useDramaSegmentsStore.getState().byEpisode[segmentKey("d9", 2)]!;
    expect(list[0]).toMatchObject({ id: "s1", status: "running", prompt: "来自推送" });
  });

  test("对账：只重新拉正在看的那几集，请求失败不抛", async () => {
    const get = spyOn(service, "get").mockResolvedValue({ items: [raw({ prompt: "对账来的" })] });
    useDramaSegmentsStore.getState().watch("d1", 3);
    await reconcileWatchedSegments();
    expect(get.mock.calls[0][0]).toBe("/dramas/d1/episodes/3/segments");
    expect(useDramaSegmentsStore.getState().byEpisode[segmentKey("d1", 3)]![0].prompt).toBe(
      "对账来的",
    );
    get.mockRejectedValue(new Error("offline"));
    await reconcileWatchedSegments();
    get.mockRestore();
  });
});

function handleShape(id: string, seq: number) {
  return {
    id,
    episodeNo: 1,
    seq,
    seconds: 10,
    prompt: "p",
    shots: [],
    keyframe: null,
    takes: [],
    chosenTaskId: "",
    status: "idle" as const,
    revision: 1,
    updatedAt: "x",
  };
}
