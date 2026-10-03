/** 节点名最多多少个字：标题行、chip、@ 菜单都要放得下 */
export const NODE_LABEL_MAX = 40;

/**
 * 重命名节点时把输入规整成能落地的名字：去首尾空白，换行和连续空白收成一个空格，
 * 超长按字符截断（不切坏 emoji）。清空了就退回原名，节点不能没有名字，@ 搜索也靠它。
 * @param input 用户输入
 * @param fallback 原来的名字
 * @returns 要写回节点的名字
 */
export function normalizeNodeLabel(input: string, fallback: string): string {
  const text = input.replace(/\s+/g, " ").trim();
  if (!text) return fallback;
  return [...text].slice(0, NODE_LABEL_MAX).join("");
}

/** 新建节点：画布里没有同名的就用种类名，有了从 2 起找空着的编号（「图片 2」） */
export function newNodeLabel(base: string, existing: Iterable<string>): string {
  const used = new Set(existing);
  if (!used.has(base)) return base;
  for (let n = 2; ; n++) if (!used.has(`${base} ${n}`)) return `${base} ${n}`;
}

const DIGITS = "零一二三四五六七八九";

/** 中文数字，用在「副本二」里；99 以上直接用阿拉伯数字 */
function chineseNumber(n: number): string {
  if (n < 10) return DIGITS[n];
  if (n >= 100) return String(n);
  const tens = Math.floor(n / 10);
  const ones = n % 10;
  return `${tens === 1 ? "" : DIGITS[tens]}十${ones === 0 ? "" : DIGITS[ones]}`;
}

/** 末尾的「 副本」「 副本二」「 副本十一」：复制副本时先去掉，免得叠成「副本 副本」 */
const COPY_SUFFIX = /\s副本[一二三四五六七八九十\d]*$/;

/** 底名 + 后缀，总长超了先截底名，保住后缀 */
function withSuffix(base: string, suffix: string) {
  const room = NODE_LABEL_MAX - [...suffix].length;
  return [...base].slice(0, Math.max(1, room)).join("") + suffix;
}

/**
 * 复制 count 份时每份的名字：一份叫「X 副本」，被占用了就往后找「X 副本二」「X 副本三」；
 * 一次好几份叫「X 副本一」「X 副本二」…，跳过已占用的。复制副本时按原名续编。
 * @param label 被复制节点的名字
 * @param existing 画布上已有的名字
 * @param count 复制几份
 */
export function copyLabels(label: string, existing: Iterable<string>, count: number): string[] {
  const base = label.replace(COPY_SUFFIX, "") || label;
  const used = new Set(existing);
  const out: string[] = [];
  const take = (name: string) => {
    if (used.has(name)) return false;
    used.add(name);
    out.push(name);
    return true;
  };
  if (count === 1 && take(withSuffix(base, " 副本"))) return out;
  for (let n = count === 1 ? 2 : 1; out.length < count; n++)
    take(withSuffix(base, ` 副本${chineseNumber(n)}`));
  return out;
}

/** 上传素材的节点名：文件名去掉扩展名（隐藏文件那种只有点开头的不去），空了用种类名 */
export function uploadLabel(fileName: string, fallback: string): string {
  const trimmed = fileName.trim();
  const dot = trimmed.lastIndexOf(".");
  const stem = dot > 0 ? trimmed.slice(0, dot) : trimmed;
  return normalizeNodeLabel(stem, fallback);
}
