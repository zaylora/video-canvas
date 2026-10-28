import { useMemo } from "react";
import { Position, useNodeConnections, useNodesData, useReactFlow } from "@xyflow/react";
import { Trash2, UserRoundX, VolumeX } from "lucide-react";

import type { CharacterDto } from "@/api/character/type";
import { NodeCard, NodeVideoBody, type NodeCardHandle } from "@/components/canvas";
import { BaseNodeContent } from "@/components/canvas/base-node";
import { CharacterCover, ExtraStack } from "@/components/characters/character-card";
import { VoiceBar } from "@/components/characters/voice-player";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { NODE_META } from "@/constants/canvas";
import { useCharacterEntry } from "@/hooks/use-characters";
import { useTaskGeneration } from "@/hooks/use-task-generation";
import { useTaskNode, type TaskNodeModel } from "@/hooks/use-task-node";
import { useCharacterReselect } from "@/store/character-reselect";
import type { CharacterEntry } from "@/store/characters";
import type { CanvasNode, CanvasNodeData } from "@/types";
import { CHARACTER_NODE_WIDTH } from "@/utils/canvas/character-nodes";
import { ignoresVoice } from "@/utils/canvas/link-rule";

import { useRegisterNodeActions } from "./node-actions";
import { NodeOverlays, NodeOverlaysPresence } from "./node-overlays";
import { useDragSelected } from "./overlay-gate";
import { useMultiSelected } from "./selection-toolbar";
import { NodeStatusLabel, TaskPromptPanel } from "./video-node";

/** 角色卡节点只有一个输出口：它不接上游，内容来自角色卡本身 */
const CHARACTER_HANDLES: NodeCardHandle[] = [{ type: "source", position: Position.Right }];

/** 已失效的节点保留一个隐藏的输出口：连线不能因为找不到 handle 被 xyflow 藏掉（设计 §7 连线保留） */
const DEAD_HANDLES: NodeCardHandle[] = [{ type: "source", position: Position.Right, hidden: true }];

/** 节点此刻的展示状态：loading 骨架，error 读取失败（有旧内容就沿用），dead 已失效，ready 正常 */
export type CharacterNodeView =
  | {
      /** 首次读取中，或读取失败且没有旧内容 */
      phase: "loading";
      /** 读取失败（而不是还在读）：显示「暂时读取失败」 */
      failed: boolean;
    }
  | {
      /** 角色卡已被删除，或主图素材已不可用 */
      phase: "dead";
      /** 失效原因，摆给用户看的那句 */
      reason: string;
    }
  | {
      /** 正常显示 */
      phase: "ready";
      /** 最新的角色卡内容 */
      card: CharacterDto;
      /** 最近一次刷新失败：内容是旧的，节点提示「暂时读取失败」 */
      stale: boolean;
    };

/**
 * 由缓存项推出节点的展示状态（设计 §7）：
 * 查不到（missing）或主图素材不可用算已失效；读到过内容的失败沿用旧内容并标 stale；
 * 没有 characterId 的旧数据算已失效；其余没读到的是加载中。
 * @param characterId 节点上的角色卡 ID
 * @param entry 缓存项
 */
export function characterNodeView(
  characterId: string | undefined,
  entry: CharacterEntry | undefined,
): CharacterNodeView {
  if (!characterId) return { phase: "dead", reason: "原角色卡已被删除" };
  if (entry?.status === "missing") return { phase: "dead", reason: "原角色卡已被删除" };
  const card = entry?.card;
  if (card) {
    // 短剧角色卡拆解出来时还没有形象，主图为空是正常的（待生成）；普通角色卡的主图没了才是素材失效
    if (!card.avatar && !card.dramaId) return { phase: "dead", reason: "形象主图已不可用" };
    return { phase: "ready", card, stale: entry.status === "error" };
  }
  return { phase: "loading", failed: entry?.status === "error" };
}

/** 角色卡节点：带 drama 元数据的（短剧资产）还能生成形象图，其余只引用角色卡 */
export function CharacterCanvasNode({
  id,
  data,
  selected,
}: {
  id: string;
  data: CanvasNodeData;
  selected?: boolean;
}) {
  return data.drama ? (
    <GenerableCharacterNode id={id} data={data} selected={selected} />
  ) : (
    <CharacterNodeShell id={id} data={data} />
  );
}

/** 短剧角色节点：和图片节点一样接生成任务（提示词面板、进度、失败重试），内容仍以角色卡为准 */
function GenerableCharacterNode({
  id,
  data,
  selected,
}: {
  id: string;
  data: CanvasNodeData;
  selected?: boolean;
}) {
  const generation = useTaskGeneration(id, "image");
  const vm = useTaskNode(id, data, "image", generation);
  useRegisterNodeActions(id, {
    blockedReason: () => vm.blockedReason,
    submit: () => vm.submit(),
  });
  const multiSelected = useMultiSelected();
  const dragSelected = useDragSelected(id);
  return (
    <>
      <CharacterNodeShell id={id} data={data} vm={vm} />
      <NodeOverlaysPresence multiSelected={multiSelected}>
        {selected && !dragSelected && (
          <NodeOverlays key="overlays" id={id} data={data}>
            {(width) => (
              <TaskPromptPanel vm={vm} data={data} kind="image" nodeId={id} width={width} />
            )}
          </NodeOverlays>
        )}
      </NodeOverlaysPresence>
    </>
  );
}

/** 生成中、排队中、转存中、失败：角色节点正文换成进度 / 失败占位 */
const SHOWS_TASK_BODY = new Set(["queued", "running", "finalizing", "failed"]);

/**
 * 角色卡节点（设计 §6.4、§7）：节点名在卡片上方（人形图标 + 角色名），卡片是 3:4 封面 + 底部声音条，
 * 右侧一个输出口。内容实时取自角色卡缓存（资产页改 / 删后自动跟着变），节点自己只存 characterId。
 * 已失效时灰显，连线保留但不参与提交，提供「重新选择」「删除节点」；
 * 连到不收参考音频的目标时，「声音未生效」标签挂在节点名旁。
 */
function CharacterNodeShell({
  id,
  data,
  vm,
}: {
  id: string;
  data: CanvasNodeData;
  /** 短剧角色节点的生成状态；普通角色卡节点没有 */
  vm?: TaskNodeModel;
}) {
  const { updateNodeData, deleteElements } = useReactFlow<CanvasNode>();
  const entry = useCharacterEntry(data.characterId);
  const view = characterNodeView(data.characterId, entry);
  const KindIcon = NODE_META.get("character")?.icon;

  const outgoing = useNodeConnections({ id, handleType: "source" });
  const targets = useNodesData<CanvasNode>(outgoing.map((connection) => connection.target));
  const voice = view.phase === "ready" ? view.card.voice : null;
  /** 连着的某个目标收图片却不收参考音频：声音在那里不生效 */
  const voiceIgnored = useMemo(
    () => !!voice && targets.some((target) => ignoresVoice(target.data)),
    [targets, voice],
  );

  const name = view.phase === "ready" ? view.card.name : (data.labelCache?.name ?? data.label);
  const dead = view.phase === "dead";

  return (
    <NodeCard
      title={dead ? "角色卡已失效" : data.label}
      onRename={dead ? undefined : (label) => updateNodeData(id, { label })}
      icon={dead ? <UserRoundX /> : KindIcon ? <KindIcon /> : undefined}
      status={
        vm && SHOWS_TASK_BODY.has(vm.view.phase) ? (
          <NodeStatusLabel view={vm.view} />
        ) : voiceIgnored ? (
          <span
            data-slot="voice-ignored-tag"
            title="目标模型不收参考音频，只带入形象"
            className="text-status-warning inline-flex items-center gap-1"
          >
            <VolumeX className="size-3.5" />
            声音未生效
          </span>
        ) : undefined
      }
      handles={dead ? DEAD_HANDLES : CHARACTER_HANDLES}
      canAcceptConnection={() => false}
      width={CHARACTER_NODE_WIDTH}
      className={dead ? "opacity-70" : undefined}
    >
      <BaseNodeContent>
        {vm && SHOWS_TASK_BODY.has(vm.view.phase) ? (
          <NodeVideoBody
            view={vm.view}
            aspect={3 / 4}
            placeholder="选中后输入提示词生成形象"
            mediaType="image"
            onCancel={vm.view.phase === "queued" ? vm.cancel : undefined}
            cancelling={vm.cancelling}
            onRetry={vm.view.phase === "failed" ? () => void vm.submit() : undefined}
            retryDisabled={!!vm.blockedReason}
            retryHint={vm.blockedReason ?? undefined}
          />
        ) : view.phase === "loading" ? (
          <>
            <div className="relative aspect-3/4 w-full">
              <Skeleton className="absolute inset-0 rounded-none motion-reduce:animate-none" />
            </div>
            <div className="flex h-10 items-center px-2.5 pb-2 text-xs">
              {view.failed ? (
                <span className="text-muted-foreground">角色卡信息暂时读取失败（{name}）</span>
              ) : (
                <Skeleton className="h-5 w-full rounded-md motion-reduce:animate-none" />
              )}
            </div>
          </>
        ) : view.phase === "dead" ? (
          <>
            <div className="text-muted-foreground bg-muted/50 flex aspect-3/4 w-full flex-col items-center justify-center gap-2 text-[13px]">
              <UserRoundX className="size-8 opacity-60" />
              {view.reason}
            </div>
            <div className="flex flex-col gap-2 px-2.5 pb-2.5">
              <span className="text-muted-foreground text-xs">连线保留，但不参与生成</span>
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => useCharacterReselect.getState().open(id)}
                >
                  重新选择
                </Button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="删除节点"
                  className="ml-auto"
                  onClick={() => void deleteElements({ nodes: [{ id }] })}
                >
                  <Trash2 />
                </Button>
              </div>
            </div>
          </>
        ) : (
          <>
            <div className="bg-muted relative aspect-3/4 w-full">
              {/* 角色卡还没有主图但刚生成出了图：先显示生成结果，主图写回后由角色卡接管 */}
              {!view.card.avatar && data.src ? (
                <img
                  src={data.src}
                  alt={name}
                  className="absolute inset-0 size-full object-cover"
                />
              ) : (
                <CharacterCover card={view.card} />
              )}
              <ExtraStack card={view.card} />
            </div>
            <div className="flex h-10 items-center px-2.5 text-xs">
              {view.stale ? (
                <span className="text-muted-foreground">角色卡信息暂时读取失败</span>
              ) : voice ? (
                <VoiceBar asset={voice} dimmed={voiceIgnored} className="w-full" />
              ) : (
                <span className="text-muted-foreground flex items-center gap-1">
                  <VolumeX className="size-3.5" />
                  未绑定声音
                </span>
              )}
            </div>
          </>
        )}
      </BaseNodeContent>
    </NodeCard>
  );
}
