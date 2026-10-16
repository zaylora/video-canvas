import { useEffect, useState } from "react";

/**
 * 输入停下来 delay 毫秒后才更新的值：搜索框输入时用它决定什么时候发请求。
 * @param value 实时的值
 * @param delay 防抖毫秒数，默认 300
 * @returns 防抖后的值
 */
export function useDebouncedValue<T>(value: T, delay = 300): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}
