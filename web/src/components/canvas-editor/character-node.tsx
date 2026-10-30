import { useEffect, useMemo } from "react";
import { Position, useNodeConnections, useNodesData, useReactFlow } from "@xyflow/react";
import { toast } from "sonner";
import {
  AudioLines,
  Ellipsis,
  FolderOpen,
  Loader2,
  Pause,
  Play,
  Plus,
  Upload,
  VolumeX,
} from "lucide-react";

import type { AssetDto } from "@/api/asset/type";
import { NodeCard, NodeVideoBody, type NodeCardHandle } from "@/components/canvas";
import type { SecondarySubmit } from "@/components/canvas/node-prompt-input";
import { BaseNodeContent } from "@/components/canvas/base-node";
import { VersionBadge } from "@/components/canvas/version-badge";
import { toggleVoice, useVoicePlaying } from "@/components/characters/voice-player";
import { Button } from "@/components/ui/button";
import { NODE_META } from "@/constants/canvas";
import { useCharacterMedia } from "@/hooks/use-character-media";
import { useMediaAspect } from "@/hooks/use-media-aspect";
import { useTaskGeneration } from "@/hooks/use-task-generation";
import { useTaskNode } from "@/hooks/use-task-node";
import { useVoiceAfterImage } from "@/hooks/use-voice-after-image";
import { useVoiceBackfill } from "@/hooks/use-voice-backfill";
import { useVoiceGeneration, type VoiceGeneration } from "@/hooks/use-voice-generation";
import { cn } from "@/lib/utils";
import { useCharacterDetail } from "@/store/character-detail";
import { usePromptEditor } from "@/store/prompt-editor";
import type { CanvasNode, CanvasNodeData } from "@/types";
import { ignoresVoice } from "@/utils/canvas/link-rule";
import { shouldRecordAspect } from "@/utils/canvas/media-aspect";
import { showVersionBadge } from "@/utils/canvas/versions";
import { nodeFrameSize, paramAspect } from "@/utils/canvas/node-frame";
import { applyPickedAssets, emptyCharacter, MAX_EXTRAS } from "@/utils/characters/node-character";
import { characterNodeAspect, characterNodeView, voiceCell } from "@/utils/characters/node-view";
import { lowestUnit } from "@/utils/pricing/quote";

import { CharacterDetailDialog } from "./character-detail-dialog";
import { useRegisterNodeActions } from "./node-actions";
import { PROMPT_DIALOG_PANEL_CLASS, PromptEditDialog } from "./prompt-edit-dialog";
import { NodeOverlays, NodeOverlaysPresence } from "./node-overlays";
import { useDragSelected } from "./overlay-gate";
import { useMultiSelected } from "./selection-toolbar";
import { LibraryPickerForCharacter } from "./character-library-picker";
import { NodeStatusLabel, TaskPromptPanel } from "./video-node";

/**
 * 角色卡节点两侧各一个口：左边接参考图 / 文本（「参考图建角色」类生成用，规则同图片节点），
 * 右边输出到图片 / 视频生成节点（形象和声音在那里展开成参考）。
 */
const CHARACTER_HANDLES: NodeCardHandle[] = [
  { type: "target", position: Position.Left },
  { type: "source", position: Position.Right },
];

/** 生成中、排队中、转存中、失败：节点正文换成进度 / 失败占位 */
const SHOWS_TASK_BODY = new Set(["queued", "running", "finalizing", "failed"]);

/**
 * 角色卡节点（设计 docs/design/角色卡/画布角色卡重设计.md §6.1）：
 * 和图片节点一样的普通节点，角色数据（名字、形象名、补充图、音色）就存在节点上、只属于当前画布。
 * 新建是空节点，封面给「上传」「素材库」两个入口，选中后下方出现图片生成面板，生成的图就是主图。
 */
export function CharacterCanvasNode({
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
  // 声音生成只在这里起一份：详情页、面板和短剧列表的「生成（含音色）」共用，模型选择、提交锁都是同一份
  const voiceGen = useVoiceGeneration(id, data);
  const voicePrompt = data.character?.voicePrompt ?? "";
  const voiceBlocked = voiceGen.blockedReason(voicePrompt);
  useVoiceAfterImage(id, data, voiceGen);
  /** 「生成（含音色）」：先提交形象，这次的任务 id 记进 voiceAfterImage，出图后用新主图接着生成音色 */
  const submitWithVoice = () =>
    voiceBlocked
      ? Promise.resolve(voiceBlocked)
      : vm.submit((taskIds) => ({ voiceAfterImage: taskIds }));
  useRegisterNodeActions(id, {
    blockedReason: () => vm.blockedReason,
    submit: () => vm.submit(),
    voice: {
      blockedReason: () => voiceBlocked,
      submitAfterImage: submitWithVoice,
    },
  });
  const withVoice: SecondarySubmit = {
    label: "含音色",
    blockedReason: vm.blockedReason ?? voiceBlocked,
    extraCredits: lowestUnit(voiceGen.model?.pricing),
    onSubmit: () => void submitWithVoice(),
  };
  const multiSelected = useMultiSelected();
  const dragSelected = useDragSelected(id);
  return (
    <>
      <CharacterNodeBody id={id} data={data} vm={vm} voiceGen={voiceGen} />
      <NodeOverlaysPresence multiSelected={multiSelected}>
        {selected && !dragSelected && (
          <NodeOverlays key="overlays" id={id} data={data}>
            {(width) => (
              <TaskPromptPanel
                vm={vm}
                data={data}
                kind="image"
                nodeId={id}
                width={width}
                secondarySubmit={withVoice}
              />
            )}
          </NodeOverlays>
        )}
      </NodeOverlaysPresence>
      <PromptEditDialog
        id={id}
        title={data.label}
        actions={
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              usePromptEditor.getState().close();
              useCharacterDetail.getState().open(id);
            }}
          >
            <AudioLines />
            角色详情与音色
          </Button>
        }
      >
        {(width) => (
          <TaskPromptPanel
            vm={vm}
            data={data}
            kind="image"
            nodeId={id}
            width={width}
            className={PROMPT_DIALOG_PANEL_CLASS}
            secondarySubmit={withVoice}
          />
        )}
      </PromptEditDialog>
    </>
  );
}

/** 声音格里的音频试听：圆形按钮，点一下播放 / 暂停（全站同一时刻只放一条） */
function AudioCell({
  asset,
  ignored,
  title,
}: {
  asset: AssetDto;
  ignored: boolean;
  title: string;
}) {
  const playing = useVoicePlaying(asset.id);
  return (
    <button
      type="button"
      title={title}
      aria-label={playing ? "暂停试听" : "试听声音"}
      aria-pressed={playing}
      onClick={(event) => {
        event.stopPropagation();
        toggleVoice(asset);
      }}
      onPointerDown={(event) => event.stopPropagation()}
      className={cn(
        "focus-visible:ring-ring/60 bg-info/15 text-info grid size-9 shrink-0 place-items-center rounded-[10px] outline-none focus-visible:ring-2 [&_svg]:size-4",
        ignored && "opacity-50",
      )}
    >
      {playing ? <Pause /> : <Play />}
    </button>
  );
}

/**
 * 节点正文：封面（空 / 生成中 / 失败 / 主图）、声音格 + 补充图格、形象名与状态标签、「…」菜单入口。
 * 连到不收参考音频的目标时，「声音未生效」标签挂在节点名旁。
 */
function CharacterNodeBody({
  id,
  data,
  vm,
  voiceGen,
}: {
  id: string;
  data: CanvasNodeData;
  vm: ReturnType<typeof useTaskNode>;
  voiceGen: VoiceGeneration;
}) {
  const { updateNodeData } = useReactFlow<CanvasNode>();
  // 详情由「…」按钮、音色格、或编辑器里双击节点打开；同一时刻只开一个
  const detailOpen = useCharacterDetail((state) => state.nodeId === id);
  const setDetailOpen = (open: boolean) =>
    open ? useCharacterDetail.getState().open(id) : useCharacterDetail.getState().close();
  const character = data.character ?? emptyCharacter(data.label);
  const view = characterNodeView(data);
  const KindIcon = NODE_META.get("character")?.icon;

  const outgoing = useNodeConnections({ id, handleType: "source" });
  const targets = useNodesData<CanvasNode>(outgoing.map((connection) => connection.target));
  const hasAudio = !!character.voice;
  /** 连着的某个目标收图片却不收参考音频：声音在那里不生效 */
  const voiceIgnored = useMemo(
    () => hasAudio && targets.some((target) => ignoresVoice(target.data)),
    [hasAudio, targets],
  );
  // 声音任务的结果由这里回填（节点始终挂载，详情页关着也能收到）
  const voiceTask = useVoiceBackfill(id, data);
  const cell = voiceCell(character, voiceIgnored, voiceTask.phase === "generating");

  /** 拿到素材后写进节点：主图落在节点自己的 assetId / src，补充图、音频落在 character 里 */
  const apply = (
    assets: AssetDto[],
    purpose: "main" | "extra" | "voice",
    origin: "upload" | "library" = "library",
  ) => {
    const { patch, warnings } = applyPickedAssets(data, assets, purpose, origin);
    if (Object.keys(patch).length > 0) updateNodeData(id, patch);
    if (warnings.length > 0) toast.warning([...new Set(warnings)].join("；"));
  };
  const media = useCharacterMedia((asset, purpose) => apply([asset], purpose, "upload"));

  // 框的画幅跟主图走：有主图量主图，量到后记进节点；没主图跟参数面板选的比例
  const measuredAspect = useMediaAspect(view.phase === "ready" ? data.src : null, "image");
  useEffect(() => {
    if (measuredAspect !== null && shouldRecordAspect(data.mediaAspect, measuredAspect)) {
      updateNodeData(id, { mediaAspect: measuredAspect });
    }
  }, [data.mediaAspect, id, measuredAspect, updateNodeData]);
  const aspect = characterNodeAspect({
    measured: measuredAspect,
    declared: data.mediaAspect,
    selected: paramAspect(vm.caps, vm.params),
  });

  // 生成过多张（或换过主图）：右上角出「历史」徽标，点开选一张当主图，和图片节点一样
  const badge = showVersionBadge(data);

  const showTask = SHOWS_TASK_BODY.has(vm.view.phase);
  const extraFull = character.extras.length >= MAX_EXTRAS;

  return (
    <NodeCard
      title={data.label}
      onRename={(label) => updateNodeData(id, { label })}
      icon={KindIcon ? <KindIcon /> : undefined}
      status={
        showTask ? (
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
      handles={CHARACTER_HANDLES}
      canAcceptConnection={() => true}
      width={nodeFrameSize(aspect).width}
      corner={badge ? <VersionBadge nodeId={id} badge={badge} /> : undefined}
    >
      <BaseNodeContent>
        <div className="relative w-full" style={{ aspectRatio: aspect }}>
          {showTask ? (
            <NodeVideoBody
              view={vm.view}
              aspect={aspect}
              placeholder="选中后输入提示词生成形象"
              mediaType="image"
              onCancel={vm.cancel}
              cancelling={vm.cancelling}
              onRetry={vm.view.phase === "failed" ? () => void vm.submit() : undefined}
              retryDisabled={!!vm.blockedReason}
              retryHint={vm.blockedReason ?? undefined}
            />
          ) : view.phase === "ready" && data.src ? (
            <img
              src={data.src}
              alt={character.name}
              draggable={false}
              className="absolute inset-0 size-full object-cover"
            />
          ) : (
            <div className="bg-muted absolute inset-0 flex flex-col items-center justify-center gap-2 px-6">
              <Button
                variant="secondary"
                size="sm"
                className="w-full max-w-40"
                disabled={media.uploading === "main"}
                onClick={() => media.pickFile("image", "main")}
              >
                <Upload />
                {media.uploading === "main" ? "上传中…" : "上传"}
              </Button>
              <Button
                variant="secondary"
                size="sm"
                className="w-full max-w-40"
                onClick={() => media.openLibrary("image", "main")}
              >
                <FolderOpen />
                素材库
              </Button>
              <span className="text-muted-foreground mt-1 text-center text-xs">
                或选中后在下方直接生成
              </span>
            </div>
          )}
        </div>

        <div className="flex items-center gap-1.5 px-2.5 pt-2.5">
          {cell.kind === "audio" && character.voice ? (
            <AudioCell
              asset={{ id: character.voice.assetId, url: character.voice.src } as AssetDto}
              ignored={cell.ignored}
              title={cell.title}
            />
          ) : (
            <button
              type="button"
              title={cell.title}
              aria-label="声音：打开详情设置"
              onClick={() => setDetailOpen(true)}
              className={cn(
                "bg-muted text-muted-foreground hover:bg-accent focus-visible:ring-ring/60 grid size-9 shrink-0 place-items-center rounded-[10px] outline-none focus-visible:ring-2 [&_svg]:size-4",
                cell.kind === "generating" && "bg-info/15 text-info",
              )}
            >
              {cell.kind === "generating" ? <Loader2 className="animate-spin" /> : <AudioLines />}
            </button>
          )}
          {character.extras.map((extra) => (
            <span
              key={extra.assetId}
              className="bg-muted size-9 shrink-0 overflow-hidden rounded-[10px]"
            >
              <img src={extra.src} alt="" draggable={false} className="size-full object-cover" />
            </span>
          ))}
          {!extraFull && (
            <button
              type="button"
              title={`添加补充图（${character.extras.length}/${MAX_EXTRAS}）`}
              aria-label="添加补充图"
              disabled={media.uploading === "extra"}
              onClick={() => media.openLibrary("image", "extra")}
              className="bg-muted text-muted-foreground hover:bg-accent focus-visible:ring-ring/60 grid size-9 shrink-0 place-items-center rounded-[10px] outline-none focus-visible:ring-2 disabled:opacity-50 [&_svg]:size-4"
            >
              <Plus />
            </button>
          )}
        </div>

        <div className="flex items-center gap-1.5 px-2.5 pt-2 pb-2.5 text-xs">
          <b className="max-w-28 truncate text-[13px]" title={character.look}>
            {character.look}
          </b>
          {view.tag && (
            <span
              className={cn(
                "rounded-md px-1.5 py-0.5 text-[10px] font-medium",
                view.phase === "failed"
                  ? "bg-destructive/15 text-destructive"
                  : view.phase === "generating"
                    ? "bg-info/15 text-info"
                    : "bg-status-warning/15 text-status-warning",
              )}
            >
              {view.tag}
            </span>
          )}
          <span className="flex-1" />
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label="编辑角色卡"
            title="编辑角色卡（双击节点也可以）"
            onClick={() => setDetailOpen(true)}
          >
            <Ellipsis />
          </Button>
        </div>
        {data.drama && (
          <div className="text-muted-foreground -mt-1.5 px-2.5 pb-2.5 text-[11px]">
            出现集数：
            {data.drama.episodes === null
              ? "未分类"
              : data.drama.episodes.length === 0
                ? "全集"
                : `第 ${data.drama.episodes.join("、")} 集`}
          </div>
        )}
      </BaseNodeContent>
      <LibraryPickerForCharacter
        request={media.library}
        data={data}
        onClose={media.closeLibrary}
        onConfirm={(assets, purpose) => apply(assets, purpose, "library")}
      />
      <CharacterDetailDialog
        id={id}
        data={data}
        voiceGen={voiceGen}
        open={detailOpen}
        onOpenChange={setDetailOpen}
      />
    </NodeCard>
  );
}
