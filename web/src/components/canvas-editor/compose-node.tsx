import { useMemo } from "react";
import { Position, useNodeConnections, useNodesData, useReactFlow, useStore } from "@xyflow/react";
import { ArrowDown, ArrowUp, Loader2 } from "lucide-react";

import { ComposeBar } from "@/components/canvas/compose-bar";
import { NodeCard, type NodeCardHandle } from "@/components/canvas";
import { BaseNodeContent } from "@/components/canvas/base-node";
import { NODE_META } from "@/constants/canvas";
import { useCanvasCompose } from "@/hooks/use-canvas-compose";
import { useCanvasArchive } from "@/hooks/use-canvas-archive";
import type { CanvasNode, CanvasNodeData } from "@/types";
import { composeInputOf } from "@/utils/canvas/compose-input";
import { DEFAULT_NODE_ASPECT } from "@/utils/canvas/node-frame";
import { useCanvasReadOnly } from "@/utils/canvas/read-only";
import { mmss } from "@/utils/storyboard/compose";

/** 只有输入口：成片是终点，拼完不再往任何节点连 */
const COMPOSE_HANDLES: NodeCardHandle[] = [{ type: "target", position: Position.Left }];

/** 位置指纹的粒度（像素）：拖动时每帧都变会让节点一直重渲染，按格子取整只在跨格时重排 */
const LAYOUT_GRAIN = 24;

/** 段号：01、02…… */
const segNo = (seq: number) => String(seq + 1).padStart(2, "0");

/**
 * 成片合成节点：把连进来的视频按顺序拼成一条。
 *
 * 顺序有两层来源，都以正文里显示的段号为准：
 * 1. 默认按画布位置（上→下、同高左→右），拖动上游节点就换顺序；
 * 2. 行上的上下移写进节点的 composeOrder，之后以它为准，新连进来的排在末尾。
 * 最终顺序在提交那一刻冻结进任务行（后端 input_json），之后调序不会让旧成片「偷偷变内容」，
 * 要换顺序就再合成一次，旧成片会被标成过期。
 */
export function ComposeCanvasNode({
  id,
  data,
}: {
  id: string;
  data: CanvasNodeData;
  selected?: boolean;
}) {
  const { updateNodeData } = useReactFlow<CanvasNode>();
  const readOnly = useCanvasReadOnly();
  const KindIcon = NODE_META.get("compose")?.icon;
  const PlaceholderIcon = NODE_META.get("compose")?.placeholderIcon;

  const connections = useNodeConnections({ id, handleType: "target" });
  const sourceIds = useMemo(() => connections.map((item) => item.source), [connections]);
  const sources = useNodesData<CanvasNode>(sourceIds);

  // 上游节点在画布上的位置：取整成字符串做指纹，值没变就不触发重渲染
  const layoutKey = useStore((state) =>
    sourceIds
      .map((sourceId) => {
        const position = state.nodeLookup.get(sourceId)?.position;
        return `${sourceId}@${Math.round((position?.y ?? 0) / LAYOUT_GRAIN)},${Math.round(
          (position?.x ?? 0) / LAYOUT_GRAIN,
        )}`;
      })
      .join("|"),
  );

  // 手动调过的顺序：拼成字符串当依赖，数组每次渲染都是新引用，直接当依赖会反复重算
  const orderKey = data.composeOrder?.join(",") ?? "";

  const input = useMemo(() => {
    const byId = new Map(sources.map((node) => [node.id, node.data]));
    const list = layoutKey
      ? layoutKey.split("|").map((item) => {
          const [sourceId, at] = item.split("@");
          const [y, x] = (at ?? "0,0").split(",").map(Number);
          return { id: sourceId, data: byId.get(sourceId), y, x };
        })
      : [];
    return composeInputOf(list, orderKey ? orderKey.split(",") : []);
  }, [layoutKey, orderKey, sources]);

  const { job, pending, busy, compose, cancel } = useCanvasCompose(id, input.candidates, "compose");
  const { archive, archiving } = useCanvasArchive();

  const succeeded = job?.status === "succeeded";

  /** 上下移：把当前显示顺序整体写回 composeOrder；没列进去的上游仍按位置接在末尾 */
  const move = (seq: number, delta: number) => {
    const ids = input.rows.map((row) => row.id);
    const to = seq + delta;
    if (to < 0 || to >= ids.length) return;
    [ids[seq], ids[to]] = [ids[to], ids[seq]];
    updateNodeData(id, { composeOrder: ids });
  };

  return (
    <NodeCard
      title={data.label}
      onRename={(label) => updateNodeData(id, { label })}
      icon={KindIcon ? <KindIcon /> : undefined}
      status={
        busy ? (
          <span className="text-status-running inline-flex items-center gap-1">
            <Loader2 className="size-3 animate-spin" />
            合成中
          </span>
        ) : succeeded ? (
          <span className="text-muted-foreground">
            成片 <span className="font-mono">{mmss(job?.duration_ms ?? 0)}</span>
          </span>
        ) : undefined
      }
      handles={COMPOSE_HANDLES}
    >
      <BaseNodeContent style={{ aspectRatio: DEFAULT_NODE_ASPECT }}>
        {input.total === 0 ? (
          <div className="bg-muted flex w-full flex-1 flex-col items-center justify-center gap-3 rounded-[inherit] px-10 text-center">
            {PlaceholderIcon && <PlaceholderIcon className="text-muted-foreground/40 size-12" />}
            <p className="text-sm font-medium">把视频节点连进来</p>
            <p className="text-muted-foreground text-xs leading-relaxed">
              默认从上到下、同高从左到右；连好后也能在这里上下调序
            </p>
          </div>
        ) : (
          <div className="flex min-h-0 flex-1 flex-col overflow-hidden p-2">
            {/* 段数写死在头顶：连了几段、几段已出片一眼看到，顺序是不是手动调过也标出来 */}
            <div className="text-muted-foreground flex shrink-0 items-center justify-between px-1.5 pb-1 text-[11px]">
              <span>
                共 <span className="text-foreground font-mono">{input.total}</span> 段
              </span>
              <span>
                <span className="text-foreground font-mono">{input.candidates.length}</span>{" "}
                段已出片
                {orderKey ? " · 已手动调序" : ""}
              </span>
            </div>
            {/* 段数不定：整列滚动，不截断成「还有 N 段」——截了后面的既看不到、也调不了序 */}
            <ul className="min-h-0 flex-1 overflow-y-auto pr-1">
              {input.rows.map((row) => (
                <li
                  key={row.id}
                  className="group hover:bg-muted/60 flex h-8 items-center gap-2 rounded-md px-1.5 text-xs"
                >
                  <span className="w-5 shrink-0 font-semibold tabular-nums">{segNo(row.seq)}</span>
                  <span className="min-w-0 flex-1 truncate">{row.label}</span>
                  {row.assetId ? (
                    <span className="bg-status-success/15 text-status-success shrink-0 rounded-md px-1.5 py-0.5 text-[11px]">
                      已出片
                    </span>
                  ) : (
                    <span className="bg-muted text-muted-foreground shrink-0 rounded-md px-1.5 py-0.5 text-[11px]">
                      待出片
                    </span>
                  )}
                  {!readOnly && (
                    // 一直显示，不靠 hover：鼠标没放上去时也得看得出这里能调序
                    <span className="flex shrink-0 items-center">
                      <button
                        type="button"
                        className="nodrag text-muted-foreground hover:text-foreground disabled:opacity-20"
                        title="上移一位"
                        aria-label={`把第 ${row.seq + 1} 段上移一位`}
                        disabled={row.seq === 0}
                        onClick={() => move(row.seq, -1)}
                      >
                        <ArrowUp className="size-3.5" />
                      </button>
                      <button
                        type="button"
                        className="nodrag text-muted-foreground hover:text-foreground disabled:opacity-20"
                        title="下移一位"
                        aria-label={`把第 ${row.seq + 1} 段下移一位`}
                        disabled={row.seq === input.rows.length - 1}
                        onClick={() => move(row.seq, 1)}
                      >
                        <ArrowDown className="size-3.5" />
                      </button>
                    </span>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
      </BaseNodeContent>
      {!readOnly && input.total > 0 && (
        <ComposeBar
          total={input.total}
          candidates={input.candidates}
          missing={input.missing}
          job={job}
          busy={busy}
          pending={pending}
          onCompose={() => void compose()}
          onCancel={() => void cancel()}
          onArchive={() => void archive()}
          archiving={archiving}
        />
      )}
    </NodeCard>
  );
}
