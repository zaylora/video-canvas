/**
 * 浏览器能不能跑导演台：要有 WebGL2。
 * 单独放一个文件、不碰 three.js：画布上打开导演台前要先判断，不能为此把 three 拖进首屏的包。
 */
export function supportsWebGL2(): boolean {
  try {
    return !!document.createElement("canvas").getContext("webgl2");
  } catch {
    return false;
  }
}
