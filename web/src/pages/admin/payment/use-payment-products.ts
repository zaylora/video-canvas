import { useCallback, useEffect, useState } from "react";

import { listPaymentProducts } from "@/api/admin/payment";
import type { PaymentProduct } from "@/api/admin/payment/type.d";

import { useAliveRef, type LoadStatus } from "../use-admin";

/**
 * 充值商品列表（含已下架，不分页，按 sort 升序）。
 * 请求错误的全局提示由拦截器弹，这里不重复。
 * @returns 商品、加载状态，以及重新拉取 / 重试 / 本地替换方法
 */
export function usePaymentProducts() {
  const aliveRef = useAliveRef();
  const [products, setProducts] = useState<PaymentProduct[]>([]);
  const [status, setStatus] = useState<LoadStatus>("loading");

  /** 重新拉取；已有数据时失败也保留旧数据。返回最新列表，失败为 null（版本冲突后据此刷新对话框） */
  const reload = useCallback(
    (): Promise<PaymentProduct[] | null> =>
      listPaymentProducts().then(
        (list) => {
          if (aliveRef.current) {
            setProducts(list);
            setStatus("ready");
          }
          return list;
        },
        () => {
          if (aliveRef.current) setStatus((prev) => (prev === "ready" ? prev : "error"));
          return null;
        },
      ),
    [aliveRef],
  );

  /** 加载失败后的“重试”：先回到加载态，再请求 */
  const retry = useCallback(() => {
    setStatus("loading");
    void reload();
  }, [reload]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { products, setProducts, status, reload, retry };
}
