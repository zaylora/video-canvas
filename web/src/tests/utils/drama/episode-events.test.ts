import { describe, expect, test } from "bun:test";

import { handleDramaEpisodeEvent, subscribeDramaChanges } from "@/utils/drama/episode-events";
import { parseServerMessage } from "@/utils/ws/socket-client";

describe("逐集完成触发工作台刷新", () => {
  test("解析分集变更事件，丢弃缺字段和错误集号", () => {
    const event = { drama_id: "d1", episode_no: 1 };
    expect(parseServerMessage(JSON.stringify({ type: "drama.episode", data: event }))).toEqual({
      type: "drama.episode",
      data: event,
    });
    for (const data of [
      null,
      {},
      { ...event, drama_id: 1 },
      { ...event, episode_no: "1" },
      { ...event, episode_no: 0 },
      { ...event, episode_no: 1.5 },
    ]) {
      expect(parseServerMessage(JSON.stringify({ type: "drama.episode", data }))).toBeNull();
    }
  });

  test("每集完成都刷新对应短剧，关闭后停止刷新", () => {
    let refreshes = 0;
    let other = 0;
    const off = subscribeDramaChanges("d1", () => {
      refreshes++;
    });
    const offOther = subscribeDramaChanges("d2", () => {
      other++;
    });
    try {
      for (let no = 1; no <= 20; no++) {
        handleDramaEpisodeEvent({ drama_id: "d1", episode_no: no });
      }
      expect(refreshes).toBe(20);
      expect(other).toBe(0);
      off();
      handleDramaEpisodeEvent({ drama_id: "d1", episode_no: 21 });
      expect(refreshes).toBe(20);
    } finally {
      off();
      offOther();
    }
  });
});
