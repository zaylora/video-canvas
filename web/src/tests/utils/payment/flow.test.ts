import { describe, expect, test } from "bun:test";

import type { MethodDto, OrderDto, ProductDto, StoreDto } from "@/api/payment/type";
import {
  formatCountdown,
  initialPayState,
  isOrderOpen,
  payReducer,
  pickDefaults,
  remainingSeconds,
  type PayState,
} from "@/utils/payment/flow";

const product = (id: number, badge: string | null = null): ProductDto => ({
  id,
  name: `商品${id}`,
  priceFen: id * 10000,
  baseCredits: id * 10000,
  bonusCredits: 0,
  badge,
});
const method = (m: "wechat" | "alipay", available = true): MethodDto => ({
  method: m,
  available,
  reason: available ? null : "承接渠道已停用",
});
const store = (over: Partial<StoreDto> = {}): StoreDto => ({
  enabled: true,
  products: [product(1), product(2, "热门"), product(3)],
  methods: [method("wechat"), method("alipay")],
  ...over,
});
const order = (over: Partial<OrderDto> = {}): OrderDto => ({
  orderNo: "VC1",
  status: "pending",
  amountFen: 10000,
  credits: 10000,
  method: "wechat",
  checkout: { mode: "qrcode", value: "weixin://x" },
  expiresAt: 2_000_000,
  creditedAt: null,
  closeReason: null,
  latePaid: false,
  abnormalReason: null,
  ...over,
});
const ready = (): PayState => payReducer(initialPayState(), { type: "loaded", store: store() });

describe("pickDefaults：默认选中", () => {
  test("优先选带角标的商品，没有就选第一个；方式选第一个可用的", () => {
    expect(pickDefaults(store())).toEqual({ productId: 2, method: "wechat" });
    expect(pickDefaults(store({ products: [product(5), product(6)] })).productId).toBe(5);
  });

  test("微信不可用时选支付宝；都不可用时方式为 null", () => {
    const only = store({ methods: [method("wechat", false), method("alipay")] });
    expect(pickDefaults(only).method).toBe("alipay");
    const none = store({ methods: [method("wechat", false), method("alipay", false)] });
    expect(pickDefaults(none).method).toBeNull();
  });

  test("没有商品时商品为 null", () => {
    expect(pickDefaults(store({ products: [] })).productId).toBeNull();
  });
});

describe("payReducer：商城数据到达", () => {
  test("首次加载：选好默认商品和方式，停在确认态", () => {
    const s = ready();
    expect(s.phase).toBe("confirm");
    expect(s.productId).toBe(2);
    expect(s.method).toBe("wechat");
  });

  test("重新加载时保留用户已选的商品和方式（它们仍然有效）", () => {
    let s = ready();
    s = payReducer(s, { type: "selectProduct", id: 3 });
    s = payReducer(s, { type: "selectMethod", method: "alipay" });
    s = payReducer(s, { type: "loaded", store: store() });
    expect(s.productId).toBe(3);
    expect(s.method).toBe("alipay");
  });

  test("已选商品被下架或已选方式变得不可用：回落到默认值", () => {
    let s = payReducer(ready(), { type: "selectProduct", id: 3 });
    s = payReducer(s, {
      type: "loaded",
      store: store({
        products: [product(1)],
        methods: [method("wechat", false), method("alipay")],
      }),
    });
    expect(s.productId).toBe(1);
    expect(s.method).toBe("alipay");
  });

  test("正在收银时重新加载商城数据，不打断当前订单", () => {
    let s = payReducer(ready(), { type: "submit" });
    s = payReducer(s, { type: "submitted", order: order() });
    s = payReducer(s, { type: "loaded", store: store() });
    expect(s.phase).toBe("checkout");
    expect(s.order?.orderNo).toBe("VC1");
  });
});

describe("payReducer：选择与下单", () => {
  test("换商品或换支付方式后必须换新的幂等键（新的一次点击不能复用旧订单）", () => {
    let s = ready();
    s = payReducer(s, { type: "submit", newKey: () => "k-submit-1" });
    expect(s.idempotencyKey).toBe("k-submit-1");
    // 下单失败回到可重选状态后，换商品要换键
    s = payReducer(s, { type: "submitFailed", message: "渠道下单失败" });
    s = payReducer(s, { type: "selectProduct", id: 3 });
    expect(s.idempotencyKey).toBe("");
    s = payReducer(s, { type: "submit", newKey: () => "k-submit-2" });
    expect(s.idempotencyKey).toBe("k-submit-2");
  });

  test("提交中再点一次（重复点击）：保持同一个幂等键，不会产生第二笔订单", () => {
    let s = ready();
    s = payReducer(s, { type: "submit", newKey: () => "k-a" });
    s = payReducer(s, { type: "submit", newKey: () => "k-b" });
    expect(s.idempotencyKey).toBe("k-a");
    expect(s.phase).toBe("submitting");
  });

  test("提交中不能改选", () => {
    let s = payReducer(ready(), { type: "submit", newKey: () => "k" });
    s = payReducer(s, { type: "selectProduct", id: 3 });
    expect(s.productId).toBe(2);
    expect(s.phase).toBe("submitting");
  });

  test("没有选方式（都不可用）时提交无效", () => {
    const s = payReducer(
      payReducer(initialPayState(), {
        type: "loaded",
        store: store({ methods: [method("wechat", false), method("alipay", false)] }),
      }),
      { type: "submit", newKey: () => "k" },
    );
    expect(s.phase).toBe("confirm");
  });

  test("下单成功：pending 进入收银态；已入账直接成功", () => {
    let s = payReducer(ready(), { type: "submit", newKey: () => "k" });
    s = payReducer(s, { type: "submitted", order: order() });
    expect(s.phase).toBe("checkout");
    expect(s.order?.orderNo).toBe("VC1");

    let t = payReducer(ready(), { type: "submit", newKey: () => "k" });
    t = payReducer(t, { type: "submitted", order: order({ status: "credited", checkout: null }) });
    expect(t.phase).toBe("success");
  });

  test("下单失败：回到确认态并带上错误提示，选择保持不变", () => {
    let s = payReducer(ready(), { type: "submit", newKey: () => "k" });
    s = payReducer(s, { type: "submitFailed", message: "支付宝下单失败，请稍后重试" });
    expect(s.phase).toBe("confirm");
    expect(s.errorMessage).toBe("支付宝下单失败，请稍后重试");
    expect(s.productId).toBe(2);
    expect(s.idempotencyKey).toBe("");
  });

  test("下单返回的订单已是 create_failed（同一幂等键重放）：当作失败，下次换键", () => {
    let s = payReducer(ready(), { type: "submit", newKey: () => "k" });
    s = payReducer(s, {
      type: "submitted",
      order: order({ status: "create_failed", checkout: null }),
    });
    expect(s.phase).toBe("confirm");
    expect(s.errorMessage).not.toBe("");
    expect(s.idempotencyKey).toBe("");
  });
});

describe("payReducer：订单状态推进", () => {
  const checkout = (): PayState => {
    let s = payReducer(ready(), { type: "submit", newKey: () => "k" });
    s = payReducer(s, { type: "submitted", order: order() });
    return s;
  };

  test("pending → credited：成功态，带上到账积分", () => {
    const s = payReducer(checkout(), {
      type: "orderUpdated",
      order: order({ status: "credited", credits: 10500, checkout: null, creditedAt: 5 }),
    });
    expect(s.phase).toBe("success");
    expect(s.order?.credits).toBe(10500);
  });

  test("pending → closed：关闭态（过期或在别处取消）", () => {
    const s = payReducer(checkout(), {
      type: "orderUpdated",
      order: order({ status: "closed", closeReason: "timeout", checkout: null }),
    });
    expect(s.phase).toBe("closed");
  });

  test("pending → abnormal：异常态，提示保留订单号让用户联系管理员", () => {
    const s = payReducer(checkout(), {
      type: "orderUpdated",
      order: order({ status: "abnormal", abnormalReason: "amount_mismatch", checkout: null }),
    });
    expect(s.phase).toBe("abnormal");
    expect(s.order?.orderNo).toBe("VC1");
  });

  test("迟到入账（关单后又付了款）：closed → credited 也要进成功态", () => {
    let s = payReducer(checkout(), {
      type: "orderUpdated",
      order: order({ status: "closed", closeReason: "timeout", checkout: null }),
    });
    s = payReducer(s, {
      type: "orderUpdated",
      order: order({ status: "credited", latePaid: true, checkout: null, creditedAt: 9 }),
    });
    expect(s.phase).toBe("success");
    expect(s.order?.latePaid).toBe(true);
  });

  test("别的订单的更新不能串进当前订单", () => {
    const before = checkout();
    const s = payReducer(before, {
      type: "orderUpdated",
      order: order({ orderNo: "VC-OTHER", status: "credited" }),
    });
    expect(s).toBe(before);
  });

  test("没有当前订单时忽略更新", () => {
    const before = ready();
    expect(payReducer(before, { type: "orderUpdated", order: order() })).toBe(before);
  });

  test("终态不会被后到的 pending 快照拉回去（乱序的轮询结果）", () => {
    let s = payReducer(checkout(), {
      type: "orderUpdated",
      order: order({ status: "credited", checkout: null, creditedAt: 5 }),
    });
    s = payReducer(s, { type: "orderUpdated", order: order({ status: "pending" }) });
    expect(s.phase).toBe("success");
  });
});

describe("payReducer：恢复与重置", () => {
  test("恢复进行中的订单（刷新页面后）：直接进收银态并对齐商品和方式", () => {
    const s = payReducer(ready(), { type: "resume", order: order({ method: "alipay" }) });
    expect(s.phase).toBe("checkout");
    expect(s.order?.orderNo).toBe("VC1");
    expect(s.method).toBe("alipay");
  });

  test("成功后「继续充值」：回到确认态，清空订单和错误，保留选择", () => {
    let s = payReducer(ready(), { type: "submit", newKey: () => "k" });
    s = payReducer(s, { type: "submitted", order: order({ status: "credited", checkout: null }) });
    s = payReducer(s, { type: "reset" });
    expect(s.phase).toBe("confirm");
    expect(s.order).toBeNull();
    expect(s.errorMessage).toBe("");
    expect(s.idempotencyKey).toBe("");
    expect(s.productId).toBe(2);
  });
});

describe("isOrderOpen", () => {
  test("creating 和 pending 是进行中，其它都不是", () => {
    expect(isOrderOpen("creating")).toBe(true);
    expect(isOrderOpen("pending")).toBe(true);
    for (const s of ["credited", "closed", "create_failed", "abnormal"] as const) {
      expect(isOrderOpen(s)).toBe(false);
    }
  });
});

describe("倒计时", () => {
  test("remainingSeconds 向上取整，过期为 0，不出现负数", () => {
    expect(remainingSeconds(10_000, 0)).toBe(10);
    expect(remainingSeconds(10_400, 0)).toBe(11);
    expect(remainingSeconds(10_000, 10_000)).toBe(0);
    expect(remainingSeconds(10_000, 20_000)).toBe(0);
  });

  test("formatCountdown 补零成 mm:ss", () => {
    expect(formatCountdown(0)).toBe("00:00");
    expect(formatCountdown(5)).toBe("00:05");
    expect(formatCountdown(65)).toBe("01:05");
    expect(formatCountdown(15 * 60)).toBe("15:00");
    expect(formatCountdown(2 * 3600)).toBe("120:00");
  });
});
