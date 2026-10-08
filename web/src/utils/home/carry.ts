/** 「做同款」要带进首页输入框的提示词存在 sessionStorage 里的键 */
const CARRY_KEY = "video-canvas:carry-prompt";

/**
 * 记下登录后要填进首页输入框的提示词。放 sessionStorage 而不是 URL，
 * 免得带进浏览器历史和服务端日志；存不进去（隐私模式等）就只是不带入，不影响登录。
 * @param text 提示词；空白不记
 */
export const saveCarry = (text: string) => {
  const value = text.trim();
  if (!value) return;
  try {
    sessionStorage.setItem(CARRY_KEY, value);
  } catch {
    /** 存不进去只是不带入 */
  }
};

/**
 * 取出并清掉带入的提示词：只会被取到一次
 * @returns 提示词；没有或读不了为空串
 */
export const takeCarry = () => {
  try {
    const value = sessionStorage.getItem(CARRY_KEY) ?? "";
    sessionStorage.removeItem(CARRY_KEY);
    return value;
  } catch {
    return "";
  }
};
