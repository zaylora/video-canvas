/**
 * 首页「更多」进所有画布页时，画布卡的 view-transition-name（设计稿 docs/design/首页 第 13 节）。
 * 同一张画布在两页上名字相同，浏览器才会把它们当成同一个元素做位置补间；
 * 名字必须是合法的 CSS 标识符，所以只留字母、数字、下划线和连字符。
 * @param key 画布 id；新建画布卡传 "new"
 */
export function canvasTransitionName(key: string) {
  return `canvas-card-${key.replace(/[^a-zA-Z0-9_-]/g, "")}`;
}
