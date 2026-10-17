import { useEffect } from "react";

/**
 * 设置浏览器标签页标题；页面切走后由下一个页面接着设置。
 * @param title 完整标题，用 utils/page-title 的方法拼
 */
export function usePageTitle(title: string) {
  useEffect(() => {
    document.title = title;
  }, [title]);
}
