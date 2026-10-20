// 画布节点尺寸规则，对应后端 backend/internal/agent/canvasgraph/size.go 的 NodeSize，
// 也与前端 web/src/utils/canvas/node-frame.ts 一致。三方读同一份测试向量
// backend/internal/tests/testdata/canvasgraph/node-size-cases.json，规则改动时要同步改。

const SHORT_SIDE = 324; // 图片、视频、音频节点框的短边
const TEXT_W = 576; // 文本和导演台节点：宽 576，比例 16:9
const TEXT_H = 324;
const GROUP_MIN_W = 200; // 组框最小尺寸
const GROUP_MIN_H = 140;

const ASPECT = /^(\d+(?:\.\d+)?):(\d+(?:\.\d+)?)$/;

/** 把「16:9」读成宽/高；Auto、空值或写法不认识返回 0。允许小数，不认全角冒号。 */
export function parseAspect(v) {
  if (v === undefined || v === null) return 0;
  const m = ASPECT.exec(String(v));
  if (!m) return 0;
  const w = Number(m[1]);
  const h = Number(m[2]);
  if (!(w > 0) || !(h > 0)) return 0;
  return w / h;
}

/** 按宽高比算节点框：短边固定 324，横图定高、竖图定宽，长边四舍五入。比例无效按 16:9。 */
export function frameSize(aspect) {
  if (!(aspect > 0) || !Number.isFinite(aspect)) aspect = 16 / 9;
  if (aspect >= 1) return { w: Math.round(SHORT_SIDE * aspect), h: SHORT_SIDE };
  return { w: SHORT_SIDE, h: Math.round(SHORT_SIDE / aspect) };
}

const num = (v) => (typeof v === "number" && Number.isFinite(v) ? v : 0);

/** 图片、视频节点的宽高比：mediaAspect → params.aspect_ratio / ratio（写了但读不懂就是 0）→ 模型默认 → 0（按 16:9）。 */
function imageAspect(node, defaultAspect) {
  const d = node.data ?? {};
  if (num(d.mediaAspect) > 0) return d.mediaAspect;
  const params = d.params;
  if (params && typeof params === "object") {
    for (const key of ["aspect_ratio", "ratio"]) {
      if (Object.hasOwn(params, key)) return parseAspect(params[key]);
    }
  }
  if (typeof defaultAspect === "function") return parseAspect(defaultAspect(String(d.model ?? "")));
  return 0;
}

/**
 * 返回节点在画布上的尺寸 {w, h}。
 * 组取自己的 width/height（不小于 200×140）；音频固定 1:1；文本和导演台固定 576×324；
 * 图片和视频按画幅算。成片实测的画幅只有前端知道，这里不处理。
 * opts.defaultAspect(model) 返回模型的默认画幅（"9:16"），不传就当不知道。
 */
export function nodeSize(node, opts = {}) {
  if (node.type === "group") {
    return { w: Math.max(GROUP_MIN_W, num(node.width)), h: Math.max(GROUP_MIN_H, num(node.height)) };
  }
  const kind = node.data?.kind;
  if (kind === "audio") return { w: SHORT_SIDE, h: SHORT_SIDE };
  if (kind === "image" || kind === "video") return frameSize(imageAspect(node, opts.defaultAspect));
  return { w: TEXT_W, h: TEXT_H };
}
