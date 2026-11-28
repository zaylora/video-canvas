import type { CollaborationResult } from "@/api/canvas/collaboration";
import type { CanvasGraphDto } from "@/api/canvas/type";

const minimumReplayBytes = 16 * 1024;
const snapshotRatio = 0.8;

/** 用追赶开始时的确认图谱估算快照开销，仅在还需要下一页时统计增量。 */
export function createCollaborationReplayBudget(confirmedGraph: CanvasGraphDto) {
  const encoder = new TextEncoder();
  let limit: number | undefined;
  let consumed = 0;
  return (events: CollaborationResult[]) => {
    limit ??= Math.max(
      minimumReplayBytes,
      Math.ceil(encoder.encode(JSON.stringify(confirmedGraph)).byteLength * snapshotRatio),
    );
    consumed += encoder.encode(JSON.stringify(events)).byteLength;
    return consumed < limit;
  };
}
