import { useCallback, useEffect, useState } from "react";

import { listPaymentChannels } from "@/api/admin/payment";
import type { PaymentChannel } from "@/api/admin/payment/type.d";

import { useAliveRef, type LoadStatus } from "../use-admin";

/**
 * 支付渠道列表（四个内置渠道，不分页）。
 * 渠道页的表格与配置抽屉、全局设置的承接下拉、订单页的渠道筛选与查单能力判断都读这一份，
 * 由页面入口持有，切页签不会重复请求。请求错误的全局提示由拦截器弹，这里不重复。
 * @returns 渠道、加载状态，以及重新拉取 / 重试方法
 */
export function usePaymentChannels() {
  const aliveRef = useAliveRef();
  const [channels, setChannels] = useState<PaymentChannel[]>([]);
  const [status, setStatus] = useState<LoadStatus>("loading");

  /** 重新拉取；已有数据时失败也保留旧数据。返回最新列表，失败为 null（版本冲突后据此刷新抽屉） */
  const reload = useCallback(
    (): Promise<PaymentChannel[] | null> =>
      listPaymentChannels().then(
        (list) => {
          if (aliveRef.current) {
            setChannels(list);
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

  /** 用后端返回的渠道视图就地替换列表里对应的一项，免得等整表刷新 */
  const replace = useCallback((view: PaymentChannel) => {
    setChannels((prev) =>
      prev.map((channel) => (channel.provider === view.provider ? view : channel)),
    );
  }, []);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { channels, status, reload, retry, replace };
}

/** 渠道列表 hook 的返回值，页签组件从入口接收它 */
export type PaymentChannels = ReturnType<typeof usePaymentChannels>;
