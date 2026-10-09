/**
 * 把同一轮提交里各节点发起的「重测连接点」请求攒成一次。
 * xyflow 的 useUpdateNodeInternals 每调用一次就排一个 rAF，每个 rAF 都会让整张画布同步重渲染一遍，
 * 200 个节点同时调用就是 200 轮、约 11 秒的长任务；合并后只剩一个 rAF、一轮渲染。
 */
export function createInternalsBatcher(flush: (ids: string[]) => void) {
  let pending: Set<string> | null = null;
  return (id: string) => {
    if (!pending) {
      const batch = new Set<string>();
      pending = batch;
      // 同一次提交里的 effect 是连着跑完的，到微任务时这一批已经收齐
      queueMicrotask(() => {
        pending = null;
        flush([...batch]);
      });
    }
    pending.add(id);
  };
}
