/** 附在错误提示上的一个动作按钮，如积分不足时的「去充值」 */
export interface ErrorAction {
  /** 按钮文字 */
  label: string;
  /** 点击后执行 */
  onClick: () => void;
}

/** 按错误码生成动作；返回 null 表示当前不适用，不显示按钮 */
type ErrorActionFactory = () => ErrorAction | null;

const factories = new Map<number, ErrorActionFactory>();

/**
 * 给某个业务错误码的全局错误提示附上一个动作按钮。
 * 请求层（request.ts）只负责按错误码查表，不依赖任何业务组件；业务模块在自己加载时注册。
 * 每次出错时才调用 factory：条件（如「支付功能是否开启」）取弹提示那一刻的最新值。
 * @param code 业务错误码
 * @param factory 生成动作；返回 null 表示不适用
 */
export function registerErrorAction(code: number, factory: ErrorActionFactory): void {
  factories.set(code, factory);
}

/**
 * 取某个错误码当前对应的动作。
 * @param code 错误码；网络错误等非数字标识没有动作
 * @returns 动作；没有注册或当前不适用返回 null
 */
export function getErrorAction(code: number | string): ErrorAction | null {
  return typeof code === "number" ? (factories.get(code)?.() ?? null) : null;
}

/** 清空全部注册（仅测试用） */
export function clearErrorActions(): void {
  factories.clear();
}
