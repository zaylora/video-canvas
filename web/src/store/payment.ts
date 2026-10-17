import { toast } from "sonner";
import { create } from "zustand";

import {
  cancelPaymentOrder,
  checkPaymentOrder,
  createPaymentOrder,
  getPaymentOrder,
  getPaymentStore,
  getPendingPaymentOrder,
} from "@/api/payment";
import type { OrderDto, PayMethod, StoreDto } from "@/api/payment/type";
import { useCreditsStore } from "@/store/credits";
import { ApiError } from "@/utils/requests/request";
import { registerErrorAction } from "@/utils/requests/error-actions";
import { formatCredits } from "@/utils/payment/format";
import {
  initialPayState,
  isOrderOpen,
  payReducer,
  type PayEvent,
  type PayState,
} from "@/utils/payment/flow";

/** 后端错误码：未完成的订单过多（65004），此时把已有的未完成订单恢复出来继续付款 */
const ERR_TOO_MANY_PENDING = 65004;
/** 同一笔订单两次主动查单的最小间隔：后端 5 秒限频，客户端留一点余量，避免触发 65014 的错误提示 */
const CHECK_MIN_INTERVAL_MS = 5500;

/** WebSocket 推送的订单变化 */
export interface PaymentWsUpdate {
  /** 订单号 */
  order_no: string;
  /** 订单当前状态 */
  status: OrderDto["status"];
  /** 到账积分（订单里的积分数） */
  credits: number;
}

interface PaymentState {
  /** 商城数据；null 表示还没加载 */
  store: StoreDto | null;
  /** 商城面板的状态机 */
  pay: PayState;
  /** 商城弹窗是否已经在显示：防止重复打开叠出多个 */
  dialogOpen: boolean;
  /** 派发事件，状态流转全在 payReducer 里 */
  dispatch: (event: PayEvent) => void;
  /** 登录后调用一次：拉商城数据（决定积分胶囊点开是商城还是余额小卡片），并恢复进行中的订单 */
  init: () => Promise<void>;
  /** 重新拉商城数据（打开商城时调用，拿到最新的商品和支付方式可用状态） */
  loadStore: () => Promise<void>;
  /** 点「立即支付」 */
  submit: () => Promise<void>;
  /** 拉当前订单的最新状态（轮询和推送都走它），只读库 */
  refreshOrder: () => Promise<void>;
  /** 向渠道主动查单（点「我已完成支付」、从付款页切回），已付款会直接入账 */
  checkNow: (opts?: { manual?: boolean }) => Promise<void>;
  /** 取消当前订单 */
  cancel: () => Promise<void>;
  /** 处理 WebSocket 推送 */
  handleWsUpdate: (update: PaymentWsUpdate) => void;
  /** 标记弹窗显示状态 */
  setDialogOpen: (open: boolean) => void;
}

/** 生成一次点击的幂等键 */
const newIdempotencyKey = () => crypto.randomUUID();

let lastCheckAt = 0;

export const usePaymentStore = create<PaymentState>((set, get) => {
  /** 派发事件并在订单到账时刷新余额 */
  const dispatch = (event: PayEvent) => {
    const before = get().pay;
    const after = payReducer(before, event);
    if (after === before) return;
    set({ pay: after });
    if (before.phase !== "success" && after.phase === "success") {
      void useCreditsStore.getState().refresh();
    }
  };

  /** 把订单更新送进状态机 */
  const apply = (order: OrderDto) => dispatch({ type: "orderUpdated", order });

  return {
    store: null,
    pay: initialPayState(),
    dialogOpen: false,
    dispatch,

    loadStore: async () => {
      const store = await getPaymentStore();
      set({ store });
      dispatch({ type: "loaded", store });
    },

    init: async () => {
      await get().loadStore();
      if (!get().store?.enabled) return;
      const pending = await getPendingPaymentOrder();
      if (pending && isOrderOpen(pending.status)) dispatch({ type: "resume", order: pending });
    },

    submit: async () => {
      dispatch({ type: "submit", newKey: newIdempotencyKey });
      const { pay } = get();
      if (pay.phase !== "submitting" || pay.productId === null || pay.method === null) return;
      try {
        const order = await createPaymentOrder({
          productId: pay.productId,
          method: pay.method,
          idempotencyKey: pay.idempotencyKey,
        });
        dispatch({ type: "submitted", order });
      } catch (error) {
        const message = error instanceof ApiError ? error.message : "下单失败，请稍后重试";
        dispatch({ type: "submitFailed", message });
        // 未完成的订单过多：把已有的恢复出来继续付款，比让用户自己去找更友好
        if (error instanceof ApiError && error.code === ERR_TOO_MANY_PENDING) {
          const pending = await getPendingPaymentOrder().catch(() => null);
          if (pending && isOrderOpen(pending.status)) dispatch({ type: "resume", order: pending });
        }
      }
    },

    refreshOrder: async () => {
      const order = get().pay.order;
      if (!order) return;
      try {
        apply(await getPaymentOrder(order.orderNo));
      } catch {
        // 轮询失败不打扰用户：全局 toast 已经提示过，下一轮或 WebSocket 推送会补上
      }
    },

    checkNow: async (opts) => {
      const order = get().pay.order;
      if (!order || get().pay.phase !== "checkout") return;
      const now = Date.now();
      if (now - lastCheckAt < CHECK_MIN_INTERVAL_MS) {
        if (opts?.manual) toast.info("刚刚查询过了，请稍等几秒再试");
        return;
      }
      lastCheckAt = now;
      try {
        const next = await checkPaymentOrder(order.orderNo);
        apply(next);
        if (opts?.manual && next.status === "pending")
          toast.info("渠道显示还没付款，付款后会自动到账");
      } catch {
        // 全局 toast 已提示；状态保持原样
      }
    },

    cancel: async () => {
      const order = get().pay.order;
      if (!order) return;
      apply(await cancelPaymentOrder(order.orderNo));
      const after = get().pay;
      if (after.phase === "closed") dispatch({ type: "reset" });
    },

    handleWsUpdate: (update) => {
      void useCreditsStore.getState().refresh();
      const current = get().pay.order;
      if (current?.orderNo === update.order_no) {
        void get().refreshOrder();
        return;
      }
      // 不是当前面板上的订单（比如弹窗已经关了）：到账了就提示一声
      if (update.status === "credited" && !get().dialogOpen) {
        toast.success(`充值成功，已到账 ${formatCredits(update.credits)} 积分`);
      }
    },

    setDialogOpen: (open) => set({ dialogOpen: open }),
  };
});

/**
 * 当前能否用商城充值：商城数据已加载、总开关开着。积分胶囊据此决定点开是商城还是余额小卡片。
 * @returns 是否开启
 */
export const usePaymentEnabled = (): boolean => usePaymentStore((s) => s.store?.enabled === true);

/**
 * 是否有一笔等待付款的订单：积分胶囊上显示小圆点提醒。
 * @returns 是否有进行中的订单
 */
export const useHasPendingOrder = (): boolean =>
  usePaymentStore((s) => s.pay.phase === "checkout" || s.pay.phase === "submitting");

/** 支付方式的展示名 */
export const METHOD_LABEL: Record<PayMethod, string> = { wechat: "微信支付", alipay: "支付宝" };

/** 后端错误码：积分不足（40001） */
const ERR_INSUFFICIENT_CREDITS = 40001;

/**
 * 积分不足的提示上附一个「去充值」：支付功能开启时才有。
 * 弹窗组件在使用处按需加载，避免请求层、store 在加载时就拉进整个商城界面。
 */
registerErrorAction(ERR_INSUFFICIENT_CREDITS, () =>
  usePaymentStore.getState().store?.enabled
    ? {
        label: "去充值",
        onClick: () =>
          void import("@/components/payment/payment-dialog").then((m) => m.openPaymentStore()),
      }
    : null,
);
