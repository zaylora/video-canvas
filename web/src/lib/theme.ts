/** 主题偏好："system" 跟随系统，其余两个是手动锁定 */
export type ThemeSetting = "system" | "light" | "dark";

const query = window.matchMedia("(prefers-color-scheme: dark)");

let current: ThemeSetting = "system";

function sync() {
  const dark = current === "system" ? query.matches : current === "dark";
  document.documentElement.classList.toggle("dark", dark);
}

// 锁定深浅色时系统怎么变都不跟，只有 system 才转播这个事件
query.addEventListener("change", () => {
  if (current === "system") sync();
});

/** 应用主题偏好，index.css 认的是 <html> 上的 .dark */
export function applyTheme(theme: ThemeSetting) {
  current = theme;
  sync();
}
