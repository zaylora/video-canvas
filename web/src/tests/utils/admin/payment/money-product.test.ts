import { describe, expect, test } from "bun:test";

import type { PaymentProduct } from "@/api/admin/payment/type.d";
import { fenToYuanInput, parseNonNegativeInt, parseYuanToFen } from "@/utils/admin/payment/money";
import {
  emptyProductForm,
  moveProductId,
  productFormToRequest,
  productToForm,
  productToRequest,
  productTotalCredits,
  validateProductForm,
} from "@/utils/admin/payment/product";

const product = (patch: Partial<PaymentProduct> = {}): PaymentProduct => ({
  id: 1,
  name: "10,000 积分",
  price_fen: 10000,
  base_credits: 10000,
  bonus_credits: 500,
  badge: "热门",
  sort: 1,
  on_sale: true,
  sold_count: 0,
  version: 3,
  created_at: "2026-10-16T00:00:00Z",
  updated_at: "2026-10-16T00:00:00Z",
  ...patch,
});

describe("parseYuanToFen 元字符串精确换算成分", () => {
  test("整数元和一两位小数", () => {
    expect(parseYuanToFen("100")).toBe(10000);
    expect(parseYuanToFen("19.9")).toBe(1990);
    expect(parseYuanToFen("19.90")).toBe(1990);
    expect(parseYuanToFen("0.01")).toBe(1);
    expect(parseYuanToFen("  8.5 ")).toBe(850);
  });

  test("浮点会出错的金额也是精确的", () => {
    // 19.9 * 100 在浮点数里是 1989.9999999999998
    expect(parseYuanToFen("19.9")).toBe(1990);
    expect(parseYuanToFen("1.15")).toBe(115);
    expect(parseYuanToFen("0.07")).toBe(7);
  });

  test("非法输入返回 null", () => {
    for (const bad of ["", " ", "-1", "1.234", "1.", ".5", "1e3", "1,000", "abc", "1 0", "+5"]) {
      expect(parseYuanToFen(bad)).toBeNull();
    }
  });

  test("超出安全整数范围返回 null", () => {
    expect(parseYuanToFen("99999999999999999999")).toBeNull();
  });
});

describe("fenToYuanInput 分转输入框文本", () => {
  test("去掉多余小数零，并能被 parseYuanToFen 还原", () => {
    expect(fenToYuanInput(10000)).toBe("100");
    expect(fenToYuanInput(1990)).toBe("19.9");
    expect(fenToYuanInput(105)).toBe("1.05");
    expect(fenToYuanInput(1)).toBe("0.01");
    for (const fen of [1, 5, 10, 99, 100, 1990, 105, 123456]) {
      expect(parseYuanToFen(fenToYuanInput(fen))).toBe(fen);
    }
  });
});

describe("parseNonNegativeInt", () => {
  test("只接受纯数字", () => {
    expect(parseNonNegativeInt("0")).toBe(0);
    expect(parseNonNegativeInt(" 120 ")).toBe(120);
    for (const bad of ["", "-1", "1.5", "1e3", "12a"]) expect(parseNonNegativeInt(bad)).toBeNull();
  });
});

describe("商品表单校验", () => {
  const ok = { name: "5,000 积分", price: "50", base: "5000", bonus: "", badge: "" };

  test("合法表单没有错误，赠送可空", () => {
    expect(validateProductForm(ok)).toEqual({});
  });

  test("名称、售价、基础积分必填，售价和基础积分要大于 0", () => {
    const errors = validateProductForm(emptyProductForm());
    expect(Object.keys(errors).sort()).toEqual(["base", "name", "price"]);
    expect(validateProductForm({ ...ok, price: "0" }).price).toBe("售价必须大于 0");
    expect(validateProductForm({ ...ok, base: "0" }).base).toBe("基础积分必须大于 0");
  });

  test("售价不能超过后端上限，超过两位小数不行", () => {
    expect(validateProductForm({ ...ok, price: "1000000.01" }).price).toBeDefined();
    expect(validateProductForm({ ...ok, price: "1000000" }).price).toBeUndefined();
    expect(validateProductForm({ ...ok, price: "9.999" }).price).toBeDefined();
  });

  test("赠送填了就必须是整数；角标最多 8 个字（按字符计）", () => {
    expect(validateProductForm({ ...ok, bonus: "1.5" }).bonus).toBeDefined();
    expect(validateProductForm({ ...ok, bonus: "0" }).bonus).toBeUndefined();
    expect(validateProductForm({ ...ok, badge: "一二三四五六七八" }).badge).toBeUndefined();
    expect(validateProductForm({ ...ok, badge: "一二三四五六七八九" }).badge).toBeDefined();
  });

  test("名称最多 64 个字，空白名称不行", () => {
    expect(validateProductForm({ ...ok, name: "  " }).name).toBeDefined();
    expect(validateProductForm({ ...ok, name: "好".repeat(65) }).name).toBeDefined();
    expect(validateProductForm({ ...ok, name: "好".repeat(64) }).name).toBeUndefined();
  });
});

describe("商品表单与请求体互转", () => {
  test("表单 → 请求体：元换成分，空赠送为 0，去首尾空格", () => {
    const body = productFormToRequest(
      { name: " 新品 ", price: "19.9", base: "2000", bonus: "", badge: " 新 " },
      true,
      0,
    );
    expect(body).toEqual({
      name: "新品",
      price_fen: 1990,
      base_credits: 2000,
      bonus_credits: 0,
      badge: "新",
      on_sale: true,
      version: 0,
    });
  });

  test("商品 → 表单 → 请求体能还原原值", () => {
    const p = product({ price_fen: 1990, bonus_credits: 0 });
    const form = productToForm(p);
    expect(form).toEqual({ name: p.name, price: "19.9", base: "10000", bonus: "", badge: "热门" });
    expect(productFormToRequest(form, p.on_sale, p.version)).toEqual(productToRequest(p));
  });

  test("上下架开关：带全部字段和 version，只翻转 on_sale", () => {
    const p = product();
    expect(productToRequest(p, { on_sale: !p.on_sale })).toEqual({
      name: p.name,
      price_fen: 10000,
      base_credits: 10000,
      bonus_credits: 500,
      badge: "热门",
      on_sale: false,
      version: 3,
    });
  });

  test("到账合计 = 基础 + 赠送", () => {
    expect(productTotalCredits(product())).toBe(10500);
  });
});

describe("moveProductId 上移下移", () => {
  test("交换相邻两项并返回新数组，不改原数组", () => {
    const ids = [1, 2, 3];
    expect(moveProductId(ids, 2, -1)).toEqual([2, 1, 3]);
    expect(moveProductId(ids, 2, 1)).toEqual([1, 3, 2]);
    expect(ids).toEqual([1, 2, 3]);
  });

  test("已在边界或找不到时返回 null", () => {
    expect(moveProductId([1, 2, 3], 1, -1)).toBeNull();
    expect(moveProductId([1, 2, 3], 3, 1)).toBeNull();
    expect(moveProductId([1, 2, 3], 9, 1)).toBeNull();
  });
});
