import type { PaymentProduct, PaymentProductRequest } from "@/api/admin/payment/type.d";

import { fenToYuanInput, parseNonNegativeInt, parseYuanToFen } from "./money";

/** 售价上限，单位分，与后端 price_fen 的 max 一致（100 万元） */
export const PRODUCT_PRICE_MAX_FEN = 100_000_000;
/** 积分上限，与后端一致 */
export const PRODUCT_CREDITS_MAX = 1_000_000_000;
/** 名称最长字数 */
export const PRODUCT_NAME_MAX = 64;
/** 角标最长字数 */
export const PRODUCT_BADGE_MAX = 8;

/** 商品表单的取值：全是输入框里的原始文本，提交前再换算 */
export interface ProductFormValues {
  /** 名称 */
  name: string;
  /** 售价，单位元 */
  price: string;
  /** 基础积分 */
  base: string;
  /** 赠送积分，可空（视为 0） */
  bonus: string;
  /** 角标，可空 */
  badge: string;
}

/** 商品表单的错误：没问题的字段不出现 */
export type ProductFormErrors = Partial<Record<keyof ProductFormValues, string>>;

/** 按字符数（不是 UTF-16 码元）计长，与后端按字符计 max 一致 */
const charCount = (text: string) => [...text].length;

/** 新建商品时的空表单 */
export const emptyProductForm = (): ProductFormValues => ({
  name: "",
  price: "",
  base: "",
  bonus: "",
  badge: "",
});

/**
 * 用已有商品填充编辑表单
 * @param product 要编辑的商品
 * @returns 表单取值；售价从分换回元文本
 */
export const productToForm = (product: PaymentProduct): ProductFormValues => ({
  name: product.name,
  price: fenToYuanInput(product.price_fen),
  base: String(product.base_credits),
  bonus: product.bonus_credits > 0 ? String(product.bonus_credits) : "",
  badge: product.badge,
});

/**
 * 校验商品表单，规则与后端 PaymentProductReq 一致
 * @param values 表单取值
 * @returns 各字段的错误；全部通过时为空对象
 */
export function validateProductForm(values: ProductFormValues): ProductFormErrors {
  const errors: ProductFormErrors = {};
  const name = values.name.trim();
  if (!name) errors.name = "请填写名称";
  else if (charCount(name) > PRODUCT_NAME_MAX)
    errors.name = `名称不能超过 ${PRODUCT_NAME_MAX} 个字`;

  const fen = parseYuanToFen(values.price);
  if (fen === null) errors.price = "请填写金额，最多两位小数";
  else if (fen < 1) errors.price = "售价必须大于 0";
  else if (fen > PRODUCT_PRICE_MAX_FEN) errors.price = "售价不能超过 1,000,000 元";

  const base = parseNonNegativeInt(values.base);
  if (base === null) errors.base = "请填写基础积分（整数）";
  else if (base < 1) errors.base = "基础积分必须大于 0";
  else if (base > PRODUCT_CREDITS_MAX) errors.base = "基础积分太大";

  if (values.bonus.trim() !== "") {
    const bonus = parseNonNegativeInt(values.bonus);
    if (bonus === null) errors.bonus = "赠送积分必须是整数";
    else if (bonus > PRODUCT_CREDITS_MAX) errors.bonus = "赠送积分太大";
  }

  if (charCount(values.badge.trim()) > PRODUCT_BADGE_MAX)
    errors.badge = `角标不能超过 ${PRODUCT_BADGE_MAX} 个字`;
  return errors;
}

/**
 * 把表单换算成请求体。调用前必须先通过 validateProductForm。
 * @param values 已通过校验的表单取值
 * @param onSale 是否上架（新建时由调用方定，编辑时沿用原值）
 * @param version 乐观锁版本；新建传 0
 * @returns 提交给后端的商品字段
 */
export function productFormToRequest(
  values: ProductFormValues,
  onSale: boolean,
  version: number,
): PaymentProductRequest {
  return {
    name: values.name.trim(),
    price_fen: parseYuanToFen(values.price) ?? 0,
    base_credits: parseNonNegativeInt(values.base) ?? 0,
    bonus_credits: parseNonNegativeInt(values.bonus) ?? 0,
    badge: values.badge.trim(),
    on_sale: onSale,
    version,
  };
}

/**
 * 由已有商品构造“只改某些字段”的请求体：后端要求带上全部字段和 version，
 * 所以上架开关也要把其余字段原样带回去。
 * @param product 当前商品
 * @param patch 要覆盖的字段
 * @returns 提交给后端的商品字段
 */
export function productToRequest(
  product: PaymentProduct,
  patch: Partial<PaymentProductRequest> = {},
): PaymentProductRequest {
  return {
    name: product.name,
    price_fen: product.price_fen,
    base_credits: product.base_credits,
    bonus_credits: product.bonus_credits,
    badge: product.badge,
    on_sale: product.on_sale,
    version: product.version,
    ...patch,
  };
}

/**
 * 到账积分合计
 * @param product 商品
 * @returns 基础 + 赠送
 */
export const productTotalCredits = (
  product: Pick<PaymentProduct, "base_credits" | "bonus_credits">,
) => product.base_credits + product.bonus_credits;

/**
 * 把某个商品上移或下移一位，返回新的 id 顺序。
 * @param ids 当前全部商品 id 的顺序
 * @param id 要移动的商品
 * @param direction -1 上移，1 下移
 * @returns 新顺序；已经在边界或找不到该商品时为 null（调用方据此禁用按钮）
 */
export function moveProductId(ids: number[], id: number, direction: -1 | 1): number[] | null {
  const from = ids.indexOf(id);
  const to = from + direction;
  if (from < 0 || to < 0 || to >= ids.length) return null;
  const next = [...ids];
  [next[from], next[to]] = [next[to], next[from]];
  return next;
}
