import type { CanvasListItemDto } from "@/api/canvas/type";

/**
 * 画布列表的内存缓存：最近一次拉到的、没带搜索词的列表。
 * 首页点「更多」进所有画布页时，转场要在新页面第一次渲染就拿到卡片当终点，不能等接口回来；
 * 所以列表页先用它渲染，接口回来再覆盖（先给旧数据、后台刷新）。
 * 按账号存：换账号读不到上一个人的画布；没登录（没有 user_id）不存。
 */
let cache: { userId: string; items: CanvasListItemDto[] } | null = null;

/** 读当前账号的缓存；没有或账号对不上返回 null */
export function readCanvasCache(userId: string | null): CanvasListItemDto[] | null {
  if (!userId || cache?.userId !== userId) return null;
  return cache.items;
}

/** 记下最新拉到的列表 */
export function writeCanvasCache(userId: string | null, items: CanvasListItemDto[]) {
  if (!userId) return;
  cache = { userId, items };
}

/** 画布删除成功后同步去掉，免得下次先看到已经删掉的卡 */
export function removeFromCanvasCache(userId: string | null, id: string) {
  if (!userId || cache?.userId !== userId) return;
  cache = { userId, items: cache.items.filter((item) => item.id !== id) };
}

/** 清空缓存（退出登录、测试） */
export function clearCanvasCache() {
  cache = null;
}
