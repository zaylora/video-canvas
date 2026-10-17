import { describe, expect, test } from "bun:test";

import { formatCredits, formatYuan } from "@/utils/payment/format";

describe("formatYuan：分换算成元", () => {
  test("整元不带小数，有零头保留两位", () => {
    expect(formatYuan(10000)).toBe("¥100");
    expect(formatYuan(5000)).toBe("¥50");
    expect(formatYuan(10050)).toBe("¥100.50");
    expect(formatYuan(5)).toBe("¥0.05");
    expect(formatYuan(10)).toBe("¥0.10");
    expect(formatYuan(0)).toBe("¥0");
  });

  test("大金额带千分位，和商城卡片上的写法一致", () => {
    expect(formatYuan(100000)).toBe("¥1,000");
    expect(formatYuan(2000000)).toBe("¥20,000");
    expect(formatYuan(123456)).toBe("¥1,234.56");
  });

  test("整数元用整数运算，不出现浮点误差", () => {
    // 0.1 + 0.2 之类的浮点问题在「分」上不存在；199 分必须是 1.99 而不是 1.9900000000000002
    expect(formatYuan(199)).toBe("¥1.99");
    expect(formatYuan(1999999)).toBe("¥19,999.99");
  });
});

describe("formatCredits：积分千分位", () => {
  test("千分位分隔", () => {
    expect(formatCredits(0)).toBe("0");
    expect(formatCredits(999)).toBe("999");
    expect(formatCredits(10000)).toBe("10,000");
    expect(formatCredits(2000000)).toBe("2,000,000");
  });
});
