/** 存偏好的最小接口：localStorage 的子集，测试里换成内存实现 */
export type FlagStorage = Pick<Storage, "getItem" | "setItem">;

const PREFIX = "director:";

/** 浏览器自带的存储；没有（服务端、测试）或被禁用时是 undefined */
const defaultStorage = (): FlagStorage | undefined => {
  try {
    return typeof localStorage === "undefined" ? undefined : localStorage;
  } catch {
    return undefined;
  }
};

/**
 * 读一个开关偏好（比如资产面板展开还是折叠）。
 * 存储不可用（隐私模式、被禁用）或没存过时给默认值，不抛错。
 */
export function readFlag(
  name: string,
  fallback: boolean,
  storage: FlagStorage | undefined = defaultStorage(),
): boolean {
  try {
    const value = storage?.getItem(PREFIX + name);
    return value === null || value === undefined ? fallback : value === "1";
  } catch {
    return fallback;
  }
}

/** 写一个开关偏好；存储不可用时什么也不做。只是为了下次打开记得，不影响功能 */
export function writeFlag(
  name: string,
  value: boolean,
  storage: FlagStorage | undefined = defaultStorage(),
) {
  try {
    storage?.setItem(PREFIX + name, value ? "1" : "0");
  } catch {
    /* 存不进去就算了：下次打开用默认值 */
  }
}
