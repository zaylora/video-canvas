/**
 * 新建画布后在新窗口打开。创建是异步的，而浏览器只允许在点击的同一个调用栈里 window.open，
 * 等接口回来再开会被当成弹窗拦掉；所以点击时先同步开一个空白窗口，创建完成后再把它带去画布。
 * 窗口被拦截时 go 返回 false，调用方改成在当前页跳转。
 */
export function openCanvasWindow() {
  const win = window.open("", "_blank");
  /** 新窗口不该能反过来操作这个页面；这里不能用 noopener 特性，那样拿不到窗口引用 */
  if (win) win.opener = null;
  return {
    /** 把空白窗口带去画布；没开成窗口时返回 false */
    go(url: string) {
      if (!win) return false;
      win.location.href = url;
      return true;
    },
    /** 创建失败：关掉空白窗口，不留一个空标签页 */
    cancel() {
      win?.close();
    },
  };
}
