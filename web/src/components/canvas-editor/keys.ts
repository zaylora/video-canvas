/** 修饰键在提示里的写法：Mac 上是 ⌘，其余是 Ctrl */
export const MOD =
  typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘" : "Ctrl+";
