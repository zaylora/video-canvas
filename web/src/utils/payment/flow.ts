import type {
  MethodDto,
  OrderDto,
  PayMethod,
  PayOrderStatus,
  ProductDto,
  StoreDto,
} from "@/api/payment/type";

/**
 * 积分商城右侧面板所处的阶段：
 * - confirm：选商品和支付方式，准备付款
 * - submitting：正在下单
 * - checkout：已下单，等用户付款（二维码 / 跳转）
 * - success：已到账
 * - closed：订单已关闭（过期、取消）
 * - abnormal：金额等核对不通过，已转人工处理
 */
export type PayPhase = "confirm" | "submitting" | "checkout" | "success" | "closed" | "abnormal";

/** 商城面板的状态：全部由事件经 payReducer 推导，没有隐藏状态 */
export interface PayState {
  /** 当前阶段 */
  phase: PayPhase;
  /** 选中的商品 ID；没有商品时为 null */
  productId: number | null;
  /** 选中的支付方式；没有可用方式时为 null */
  method: PayMethod | null;
  /** 当前订单；confirm 阶段为 null */
  order: OrderDto | null;
  /** 本次下单的幂等键；空串表示还没点过「立即支付」，换商品、换方式或失败后会清空 */
  idempotencyKey: string;
  /** 下单失败的提示，展示在确认面板里；没有为空串 */
  errorMessage: string;
}

/** 影响状态的事件 */
export type PayEvent =
  /** 商城数据到达（首次加载或刷新） */
  | { type: "loaded"; store: StoreDto }
  /** 选商品 */
  | { type: "selectProduct"; id: number }
  /** 选支付方式 */
  | { type: "selectMethod"; method: PayMethod }
  /** 点「立即支付」：newKey 只在还没有幂等键时才会被调用 */
  | { type: "submit"; newKey?: () => string }
  /** 下单成功，带回订单 */
  | { type: "submitted"; order: OrderDto }
  /** 下单请求失败 */
  | { type: "submitFailed"; message: string }
  /** 订单状态更新（轮询、WebSocket 推送、主动查单） */
  | { type: "orderUpdated"; order: OrderDto }
  /** 恢复一笔进行中的订单（刷新页面或重新打开商城） */
  | { type: "resume"; order: OrderDto }
  /** 回到确认态（成功后继续充值、关闭后重新选择） */
  | { type: "reset" };

/** 初始状态：还没有商城数据 */
export const initialPayState = (): PayState => ({
  phase: "confirm",
  productId: null,
  method: null,
  order: null,
  idempotencyKey: "",
  errorMessage: "",
});

/**
 * 订单是否还在进行中（占用用户的未支付名额）。
 * @param status 订单状态
 * @returns creating 和 pending 为 true
 */
export const isOrderOpen = (status: PayOrderStatus): boolean =>
  status === "creating" || status === "pending";

/**
 * 默认选中项：商品优先选带角标的（运营主推的那一档），没有就选第一个；支付方式选第一个可用的。
 * @param store 商城数据
 * @returns 默认商品 ID 和支付方式，没有则为 null
 */
export function pickDefaults(store: StoreDto): {
  productId: number | null;
  method: PayMethod | null;
} {
  const featured = store.products.find((p) => p.badge) ?? store.products[0];
  return { productId: featured?.id ?? null, method: firstAvailable(store.methods) };
}

/** 第一个可用的支付方式 */
const firstAvailable = (methods: MethodDto[]): PayMethod | null =>
  methods.find((m) => m.available)?.method ?? null;

/** 按订单状态推导面板阶段 */
function phaseOf(status: PayOrderStatus): PayPhase {
  switch (status) {
    case "pending":
    case "creating":
      return "checkout";
    case "credited":
      return "success";
    case "closed":
      return "closed";
    case "abnormal":
      return "abnormal";
    default:
      return "confirm";
  }
}

/** 订单状态是否已经到终态（pending / creating 以外）：终态之后不再被乱序的旧快照拉回 */
const isFinalPhase = (phase: PayPhase) =>
  phase === "success" || phase === "closed" || phase === "abnormal";

/** 终态里允许被覆盖的唯一情形：关单后又收到付款，closed → credited */
const canLeaveFinal = (from: PayPhase, to: PayPhase) => from === "closed" && to === "success";

/** 刷新商城数据时修正选择：已选项仍然有效就保留，否则回落到默认值 */
function reconcileSelection(
  state: PayState,
  store: StoreDto,
): Pick<PayState, "productId" | "method"> {
  const def = pickDefaults(store);
  const productOk = store.products.some((p) => p.id === state.productId);
  const methodOk = store.methods.some((m) => m.method === state.method && m.available);
  return {
    productId: productOk ? state.productId : def.productId,
    method: methodOk ? state.method : def.method,
  };
}

/** 只有在可以重新选择的阶段，选择变化才生效 */
const canEditSelection = (phase: PayPhase) => phase === "confirm";

/**
 * 商城面板的状态流转。纯函数：同样的状态和事件永远得到同样的结果，副作用（请求、定时器）都在 store 里。
 * 规则要点：
 * - 幂等键绑定「一次点击」：换商品、换方式、下单失败后都要清空，下次点击生成新键；提交中重复点击沿用同一个键；
 * - 订单终态不会被乱序到达的旧快照（轮询、推送先后顺序不定）拉回 pending，唯一的例外是关单后迟到入账；
 * - 别的订单的更新一律忽略。
 * @param state 当前状态
 * @param event 事件
 * @returns 新状态；没有变化时返回同一个对象
 */
export function payReducer(state: PayState, event: PayEvent): PayState {
  switch (event.type) {
    case "loaded": {
      // 正在收银时不打断：只有确认态才按新数据修正选择
      if (state.phase !== "confirm") return state;
      return { ...state, ...reconcileSelection(state, event.store) };
    }
    case "selectProduct": {
      if (!canEditSelection(state.phase) || state.productId === event.id) return state;
      return { ...state, productId: event.id, idempotencyKey: "", errorMessage: "" };
    }
    case "selectMethod": {
      if (!canEditSelection(state.phase) || state.method === event.method) return state;
      return { ...state, method: event.method, idempotencyKey: "", errorMessage: "" };
    }
    case "submit": {
      if (state.phase === "submitting") return state; // 重复点击：沿用同一个键，不会产生第二笔订单
      if (state.phase !== "confirm" || state.productId === null || state.method === null)
        return state;
      const key = state.idempotencyKey || event.newKey?.() || "";
      return { ...state, phase: "submitting", idempotencyKey: key, errorMessage: "" };
    }
    case "submitted":
      return applySubmitted(state, event.order);
    case "submitFailed":
      return { ...state, phase: "confirm", idempotencyKey: "", errorMessage: event.message };
    case "orderUpdated":
      return applyOrderUpdate(state, event.order);
    case "resume":
      return {
        ...state,
        phase: phaseOf(event.order.status),
        order: event.order,
        method: event.order.method,
        errorMessage: "",
      };
    case "reset":
      return { ...state, phase: "confirm", order: null, idempotencyKey: "", errorMessage: "" };
  }
}

/** 下单成功后的状态：create_failed 是同一幂等键重放得到的失败订单，当作下单失败处理 */
function applySubmitted(state: PayState, order: OrderDto): PayState {
  if (order.status === "create_failed") {
    return {
      ...state,
      phase: "confirm",
      order: null,
      idempotencyKey: "",
      errorMessage: "下单失败，请稍后重试或换一种支付方式",
    };
  }
  return { ...state, phase: phaseOf(order.status), order, errorMessage: "" };
}

/** 订单更新：只认当前订单，终态不回退 */
function applyOrderUpdate(state: PayState, order: OrderDto): PayState {
  if (!state.order || state.order.orderNo !== order.orderNo) return state;
  const next = phaseOf(order.status);
  if (isFinalPhase(state.phase) && !canLeaveFinal(state.phase, next)) return state;
  return { ...state, phase: next, order };
}

/**
 * 距离到期还有几秒：向上取整，已过期返回 0。
 * @param expiresAt 到期时间，毫秒时间戳
 * @param now 当前时间，毫秒时间戳
 * @returns 剩余秒数，不会是负数
 */
export const remainingSeconds = (expiresAt: number, now: number): number =>
  Math.max(0, Math.ceil((expiresAt - now) / 1000));

/**
 * 倒计时文本，补零成 mm:ss。
 * @param seconds 剩余秒数
 * @returns 如 14:05
 */
export function formatCountdown(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
}

/**
 * 从商城数据里按 ID 找商品。
 * @param store 商城数据
 * @param id 商品 ID
 * @returns 商品；找不到返回 undefined
 */
export const findProduct = (store: StoreDto | null, id: number | null): ProductDto | undefined =>
  id === null ? undefined : store?.products.find((p) => p.id === id);
