// harness 测试共用的小工具：造画布、造事件。

/** 组：x、y 是画布绝对坐标，w、h 是组框尺寸。 */
export const group = (id, x, y, w, h) => ({
  id,
  type: "group",
  position: { x, y },
  width: w,
  height: h,
  data: { label: id },
});

/**
 * 普通节点。x、y 在组内是相对组左上角的坐标。
 * opts: parent 所属组、aspect 比例参数（9:16）、prompt、outputs 产物数量、src、data 额外字段。
 */
export function node(id, kind, x, y, opts = {}) {
  const data = { kind, label: id, ...(opts.data ?? {}) };
  if (opts.aspect) data.params = { ...(data.params ?? {}), aspect_ratio: opts.aspect };
  if (opts.prompt !== undefined) {
    data.prompt = opts.prompt;
    data.params = { ...(data.params ?? {}), prompt: opts.prompt };
  }
  if (opts.outputs) data.outputs = Array.from({ length: opts.outputs }, (_, i) => ({ url: `o${i}` }));
  if (opts.src) data.src = opts.src;
  const n = { id, type: "canvas", position: { x, y }, data };
  if (opts.parent) n.parentId = opts.parent;
  return n;
}

export const edge = (id, source, target) => ({ id, source, target });
export const canvas = (nodes, edges = []) => ({ nodes, edges });

/** 一个合格的三镜头竖屏分镜：3 组，每组 文本 + 竖图 + 竖视频，组框刚好包住。 */
export function goodStoryboard() {
  const nodes = [];
  const edges = [];
  for (let i = 1; i <= 3; i++) {
    const gid = `g${i}`;
    // 组内：文本 576×324，图 324×576，视频 324×576，横排间距 120；组框 = 48+576+120+324+120+324+48 宽、72+576+48 高
    nodes.push(group(gid, 0, (i - 1) * 900, 48 + 576 + 120 + 324 + 120 + 324 + 48, 72 + 576 + 48));
    nodes.push(node(`${gid}t`, "script", 48, 72, { parent: gid, prompt: "镜头文本" }));
    nodes.push(node(`${gid}i`, "image", 48 + 576 + 120, 72, { parent: gid, aspect: "9:16", prompt: "关键帧" }));
    nodes.push(node(`${gid}v`, "video", 48 + 576 + 120 + 324 + 120, 72, { parent: gid, aspect: "9:16", prompt: "视频" }));
    edges.push(edge(`${gid}e1`, `${gid}t`, `${gid}i`), edge(`${gid}e2`, `${gid}i`, `${gid}v`));
  }
  return canvas(nodes, edges);
}

/** 造一条后端事件（和 GET /agent/sessions/:sid/events 返回的一项同形）。 */
export const ev = (seq, type, data = {}, runId = "r1") => ({
  session_id: "s1",
  canvas_id: "c1",
  run_id: runId,
  seq,
  type,
  data,
  created_at: new Date(Date.UTC(2026, 9, 20, 8, 0, seq)).toISOString(),
});

/** 一个审批视图（approval.created 的 data）。 */
export const approval = (id, kind, payload = {}, extra = {}) => ({
  id,
  run_id: "r1",
  kind,
  status: "pending",
  payload,
  quote_credits: 0,
  decision: null,
  expires_at: "2026-10-21T08:00:00Z",
  ...extra,
});
