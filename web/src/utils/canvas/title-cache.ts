/**
 * 画布名缓存：列表页、画布页加载时顺手记下，
 * 任务在别的页面完成时弹 toast 要用到画布名，不必再发请求。
 */
const titles = new Map<string, string>();

export const rememberCanvasTitle = (id: string | number, title: string) => {
  titles.set(String(id), title);
};

export const getCanvasTitle = (id: string | number | null | undefined) =>
  id === null || id === undefined ? undefined : titles.get(String(id));
