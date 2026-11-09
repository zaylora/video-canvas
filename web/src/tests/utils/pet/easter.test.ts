import { describe, expect, test } from "bun:test";

import { EASTER_LINES } from "@/constants/pet";
import {
  claimFirstOpenToday,
  claimNightEgg,
  dayKey,
  isNightHour,
  nightKey,
  pickEasterLine,
  type StampStorage,
} from "@/utils/pet/easter";

const memory = (
  init: Record<string, string> = {},
): StampStorage & { data: Record<string, string> } => {
  const data = { ...init };
  return {
    data,
    getItem: (k) => data[k] ?? null,
    setItem: (k, v) => {
      data[k] = v;
    },
  };
};
const broken: StampStorage = {
  getItem: () => {
    throw new Error("denied");
  },
  setItem: () => {
    throw new Error("denied");
  },
};
const at = (y: number, m: number, d: number, h = 12, min = 0) => new Date(y, m - 1, d, h, min);

describe("日期与夜间判定", () => {
  test("dayKey 用本地日期，补零", () => {
    expect(dayKey(at(2026, 1, 5))).toBe("2026-01-05");
  });

  test("23:00–05:00 算夜间，边界：22:59 不算、23:00 算、04:59 算、05:00 不算", () => {
    expect(isNightHour(at(2026, 11, 6, 22, 59))).toBe(false);
    expect(isNightHour(at(2026, 11, 6, 23, 0))).toBe(true);
    expect(isNightHour(at(2026, 11, 7, 4, 59))).toBe(true);
    expect(isNightHour(at(2026, 11, 7, 5, 0))).toBe(false);
  });

  test("夜晚编号：零点之后仍算前一天晚上", () => {
    expect(nightKey(at(2026, 11, 6, 23, 30))).toBe("2026-11-06");
    expect(nightKey(at(2026, 11, 7, 1, 0))).toBe("2026-11-06");
    expect(nightKey(at(2026, 11, 7, 12, 0))).toBeNull();
  });
});

describe("每天第一次打开", () => {
  test("当天第一次返回 true 并记下日期，同一天再来返回 false，隔天又是 true", () => {
    const s = memory();
    expect(claimFirstOpenToday(at(2026, 11, 6, 9), s)).toBe(true);
    expect(claimFirstOpenToday(at(2026, 11, 6, 15), s)).toBe(false);
    expect(claimFirstOpenToday(at(2026, 11, 7, 9), s)).toBe(true);
  });

  test("localStorage 读写抛错时不冒彩蛋，也不抛错", () => {
    expect(claimFirstOpenToday(at(2026, 11, 6, 9), broken)).toBe(false);
  });

  test("能读不能写：写失败不影响本次结果", () => {
    const s = memory();
    s.setItem = () => {
      throw new Error("quota");
    };
    expect(claimFirstOpenToday(at(2026, 11, 6, 9), s)).toBe(true);
  });
});

describe("每晚第一次", () => {
  test("白天不触发，也不写记录", () => {
    const s = memory();
    expect(claimNightEgg(at(2026, 11, 6, 12), s)).toBe(false);
    expect(s.data).toEqual({});
  });

  test("同一晚只第一次触发，跨零点仍是同一晚；第二天晚上再触发", () => {
    const s = memory();
    expect(claimNightEgg(at(2026, 11, 6, 23, 10), s)).toBe(true);
    expect(claimNightEgg(at(2026, 11, 7, 0, 30), s)).toBe(false);
    expect(claimNightEgg(at(2026, 11, 7, 23, 10), s)).toBe(true);
  });

  test("存储不可用时不触发", () => {
    expect(claimNightEgg(at(2026, 11, 6, 23, 10), broken)).toBe(false);
  });
});

describe("彩蛋短句库", () => {
  test("每类 3 到 5 句，且都不含 {n} 占位", () => {
    for (const lines of Object.values(EASTER_LINES)) {
      expect(lines.length).toBeGreaterThanOrEqual(3);
      expect(lines.length).toBeLessThanOrEqual(5);
      for (const l of lines) expect(l.includes("{n}")).toBe(false);
    }
  });

  test("pickEasterLine 按随机数选句，随机数接近 1 也不越界", () => {
    expect(pickEasterLine("wake", () => 0)).toBe(EASTER_LINES.wake[0]!);
    const last = EASTER_LINES.night.length - 1;
    expect(pickEasterLine("night", () => 0.999)).toBe(EASTER_LINES.night[last]!);
  });
});
