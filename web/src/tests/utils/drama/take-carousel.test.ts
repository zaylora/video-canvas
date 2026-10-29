import { describe, expect, test } from "bun:test";

import { initialTakeIndex, snapTakeIndex } from "@/utils/drama/take-carousel";

const take = (taskId: string) => ({ taskId });

describe("initialTakeIndex：轮播一打开停在哪一版", () => {
  test("有采用的：停在采用的那一版", () => {
    expect(initialTakeIndex([take("a"), take("b"), take("c")], "b")).toBe(1);
  });

  test("没有采用（或采用的不在列表里）：停在最新一版", () => {
    expect(initialTakeIndex([take("a"), take("b")], "")).toBe(1);
    expect(initialTakeIndex([take("a"), take("b")], "gone")).toBe(1);
  });

  test("一版都没有：0", () => {
    expect(initialTakeIndex([], "")).toBe(0);
  });
});

describe("snapTakeIndex：松手后被吸到哪一版", () => {
  const step = 200;

  test("慢慢松手：吸到离中间最近的一版", () => {
    // x 是轨道位移：停在第 i 版时 x = -i * step
    expect(snapTakeIndex(-260, 0, step, 4)).toBe(1);
    expect(snapTakeIndex(-320, 0, step, 4)).toBe(2);
  });

  test("用力一甩：按惯性多滑过去，但不超过两头", () => {
    expect(snapTakeIndex(-210, -2000, step, 4)).toBe(3);
    expect(snapTakeIndex(-210, 3000, step, 4)).toBe(0);
  });

  test("轻轻一拨也能换一版：速度够快时至少走一格", () => {
    expect(snapTakeIndex(-30, -500, step, 4, 0)).toBe(1);
    expect(snapTakeIndex(-570, 500, step, 4, 3)).toBe(2);
  });

  test("只有一版：始终是 0", () => {
    expect(snapTakeIndex(-999, -9999, step, 1)).toBe(0);
  });
});
