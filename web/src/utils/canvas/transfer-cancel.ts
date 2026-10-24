/** 一次导入 / 导出的取消控制器 */
export type TransferCancel = {
  /** 交给请求的取消信号 */
  signal: AbortSignal;
  /** 中止请求；重复调用只生效一次 */
  cancel: () => void;
  /** 是否已经请求取消 */
  cancelled: () => boolean;
};

/**
 * 创建一次导入 / 导出专用的取消控制器。
 * 每次流程都新建一个：上一次取消过的信号不能复用，否则下一次一发请求就会被立刻中止。
 * 取消只表示「不再等待并尽量让后端停下」，不保证后端没有做完；
 * 请求恰好在取消生效前已经成功返回时，流程应按成功处理（见设计稿 §14.8）。
 */
export function createTransferCancel(): TransferCancel {
  const controller = new AbortController();
  return {
    signal: controller.signal,
    cancel: () => controller.abort(),
    cancelled: () => controller.signal.aborted,
  };
}
