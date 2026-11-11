import { describe, expect, test } from "bun:test";

import type { AnnouncementDto } from "@/api/announcement/type";
import { mapAnnouncement } from "@/api/announcement";
import {
  DISMISS_LIMIT,
  dismiss,
  dwellMs,
  marqueePlan,
  parseCachedAnnouncements,
  parseDismissed,
  ROTATE_MS,
  shouldRefetch,
  visibleAnnouncements,
} from "@/utils/home/announcement";

const NOW = Date.parse("2026-11-12T20:00:00Z");

const item = (over: Partial<AnnouncementDto> = {}): AnnouncementDto => ({
  id: 1,
  title: "公告",
  endsAt: null,
  version: 100,
  ...over,
});

describe("公告：后端数据适配", () => {
  test("snake_case 转 camelCase，结束时间转毫秒，null 保持 null", () => {
    expect(
      mapAnnouncement({
        id: 3,
        title: "t",
        ends_at: "2026-11-13T00:00:00Z",
        version: 9,
      }),
    ).toEqual({
      id: 3,
      title: "t",
      endsAt: Date.parse("2026-11-13T00:00:00Z"),
      version: 9,
    });
    expect(
      mapAnnouncement({
        id: 1,
        title: "",
        ends_at: null,
        version: 1,
      }).endsAt,
    ).toBeNull();
  });
});

describe("公告：哪些要显示", () => {
  test("去掉已过结束时间的（恰好此刻算已结束）", () => {
    const list = [item({ id: 1 }), item({ id: 2, endsAt: NOW }), item({ id: 3, endsAt: NOW + 1 })];
    expect(visibleAnnouncements(list, [], NOW).map((a) => a.id)).toEqual([1, 3]);
  });

  test("去掉本机关闭过且版本没变的；内容改过（版本变了）重新出现", () => {
    const list = [item({ id: 1, version: 100 }), item({ id: 2, version: 200 })];
    expect(
      visibleAnnouncements(
        list,
        [
          [1, 100],
          [2, 199],
        ],
        NOW,
      ).map((a) => a.id),
    ).toEqual([2]);
  });

  test("每条都能关：关过的不再显示", () => {
    const list = [item({ id: 1 })];
    expect(visibleAnnouncements(list, [[1, 100]], NOW)).toHaveLength(0);
  });
});

describe("公告：本机关闭记录", () => {
  test("解析坏数据不抛错，只保留 [数字, 数字] 的条目", () => {
    expect(parseDismissed(null)).toEqual([]);
    expect(parseDismissed("not json")).toEqual([]);
    expect(parseDismissed('{"1":100}')).toEqual([]);
    expect(parseDismissed('[[1,100],[2,"x"],[3],"y",[4,5]]')).toEqual([
      [1, 100],
      [4, 5],
    ]);
  });

  test("关闭一条返回新数组，同一条再次关闭会挪到最后并更新版本", () => {
    const before: Array<[number, number]> = [
      [1, 1],
      [2, 2],
    ];
    const after = dismiss(before, 1, 5);
    expect(before).toEqual([
      [1, 1],
      [2, 2],
    ]);
    expect(after).toEqual([
      [2, 2],
      [1, 5],
    ]);
  });

  test(`最多保留 ${DISMISS_LIMIT} 条，挤掉最早关闭的（id 大小不影响先后）`, () => {
    let list: Array<[number, number]> = [];
    for (let i = DISMISS_LIMIT + 3; i >= 1; i -= 1) list = dismiss(list, i, i);
    expect(list).toHaveLength(DISMISS_LIMIT);
    expect(list[0][0]).toBe(DISMISS_LIMIT);
    expect(list.at(-1)?.[0]).toBe(1);
  });
});

describe("公告：放不下才滚", () => {
  test("放得下（溢出不超过 2px）不滚，停留 5 秒", () => {
    expect(marqueePlan(0)).toBeNull();
    expect(marqueePlan(2)).toBeNull();
    expect(dwellMs(null)).toBe(ROTATE_MS);
  });

  test("放不下：先停 1.5 秒，按 60px/秒滚完溢出 + 16px，末尾再停 1.5 秒", () => {
    const plan = marqueePlan(104);
    expect(plan).toEqual({ travel: 120, runMs: 2000, totalMs: 5000 });
  });

  test("停留时长取 5 秒和滚完所需时间的较大者", () => {
    expect(dwellMs(marqueePlan(14))).toBe(ROTATE_MS);
    expect(dwellMs(marqueePlan(584))).toBe(13000);
  });
});

describe("公告：何时重新拉取", () => {
  test("从没拉过要拉；距上次不足 5 分钟不拉；满 5 分钟要拉", () => {
    expect(shouldRefetch(null, NOW)).toBe(true);
    expect(shouldRefetch(NOW - 4 * 60_000, NOW)).toBe(false);
    expect(shouldRefetch(NOW - 5 * 60_000, NOW)).toBe(true);
  });
});

describe("公告：本机缓存", () => {
  test("合法缓存原样还原，坏数据回落空数组，形状不对的条目丢掉", () => {
    const good = [item({ id: 7 })];
    expect(parseCachedAnnouncements(JSON.stringify(good))).toEqual(good);
    expect(parseCachedAnnouncements(null)).toEqual([]);
    expect(parseCachedAnnouncements("{")).toEqual([]);
    expect(parseCachedAnnouncements(JSON.stringify([{ id: "x" }, item({ id: 8 })]))).toEqual([
      item({ id: 8 }),
    ]);
  });
});
