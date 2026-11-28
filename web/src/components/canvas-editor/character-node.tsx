import { useEffect } from "react";
import { Position, useReactFlow } from "@xyflow/react";
import { toast } from "sonner";
import { AudioLines, Ellipsis, FolderOpen, Loader2, Pause, Play, Plus, Upload } from "lucide-react";

import type { AssetDto } from "@/api/asset/type";
import { NodeCard, NodeVideoBody, type NodeCardHandle } from "@/components/canvas";
import type { SecondarySubmit } from "@/components/canvas/node-prompt-input";
import { BaseNodeContent } from "@/components/canvas/base-node";
import { NodeImage } from "@/components/canvas/node-image";
import { VersionBadge } from "@/components/canvas/version-badge";
import { toggleVoice, useVoicePlaying } from "@/components/characters/voice-player";
import { Button } from "@/components/ui/button";
import { NODE_META } from "@/constants/canvas";
import { useCharacterMedia } from "@/hooks/use-character-media";
import { useMediaAspect } from "@/hooks/use-media-aspect";
import { useTaskGeneration } from "@/hooks/use-task-generation";
import { useTaskNode } from "@/hooks/use-task-node";
import { useCollaborativeVoiceAfterImage, useVoiceAfterImage } from "@/hooks/use-voice-after-image";
import { useVoiceBackfill } from "@/hooks/use-voice-backfill";
import { useVoiceGeneration, type VoiceGeneration } from "@/hooks/use-voice-generation";
import { cn } from "@/lib/utils";
import { useCharacterDetail } from "@/store/character-detail";
import { usePromptEditor } from "@/store/prompt-editor";
import type { CanvasNode, CanvasNodeData } from "@/types";
import { shouldRecordAspect } from "@/utils/canvas/media-aspect";
import { showVersionBadge } from "@/utils/canvas/versions";
import { nodeFrameSize, sharedParamAspect } from "@/utils/canvas/node-frame";
import { useSharedCanvas } from "@/utils/canvas/read-only";
import {
  applyPickedAssets,
  characterImageUploaded,
  emptyCharacter,
  MAX_EXTRAS,
} from "@/utils/characters/node-character";
import { characterNodeAspect, characterNodeView, voiceCell } from "@/utils/characters/node-view";
import { lowestUnit } from "@/utils/pricing/quote";

import { CharacterDetailDialog } from "./character-detail-dialog";
import { useRegisterNodeActions } from "./node-actions";
import {
  PROMPT_DIALOG_ACTION_CLASS,
  PROMPT_DIALOG_PANEL_CLASS,
  PromptEditDialog,
} from "./prompt-edit-dialog";
import { NodeOverlays, NodeOverlaysPresence } from "./node-overlays";
import { useDragSelected } from "./overlay-gate";
import { useMultiSelected } from "./selection-toolbar";
import { LibraryPickerForCharacter } from "./character-library-picker";
import { NodeStatusLabel, TaskPromptPanel } from "./video-node";
import { SharedDetailsButton } from "./shared-node-details";

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
export function CharacterCanvasNode(props: {
  id: string;
  data: CanvasNodeData;
  selected?: boolean;
}) {
  const shared = useSharedCanvas();
  return shared ? <SharedCharacterNode data={props.data} /> : <EditableCharacterNode {...props} />;
}

function SharedCharacterNode({ data }: { data: CanvasNodeData }) {
  const character = data.character ?? emptyCharacter(data.label);
  const KindIcon = NODE_META.get("character")?.icon;
  const measured = useMediaAspect(data.src, "image");
  const aspect = characterNodeAspect({
    measured,
    declared: data.mediaAspect,
    selected: sharedParamAspect(data),
  });
  return (
    <NodeCard
      title={data.label}
      icon={KindIcon ? <KindIcon /> : undefined}
      handles={CHARACTER_HANDLES}
      width={nodeFrameSize(aspect).width}
    >
      <BaseNodeContent>
        <div className="bg-muted relative w-full" style={{ aspectRatio: aspect }}>
          {data.src ? (
            <CharacterCover src={data.src} name={character.name} />
          ) : (
            <p className="text-muted-foreground grid size-full place-items-center text-xs">
              暂无形象
            </p>
          )}
        </div>
        <div className="flex items-center gap-1.5 px-2.5 pt-2.5">
          {character.voice && (
            <AudioCell
              asset={{ id: character.voice.assetId, url: character.voice.src } as AssetDto}
              name={character.voice.name}
            />
          )}
          <CharacterExtraImages extras={character.extras} />
        </div>
        <div className="flex items-center justify-between gap-1.5 px-2.5 pt-2 pb-2.5 text-xs">
          <b className="truncate text-[13px]" title={character.look}>
            {character.look}
          </b>
          <SharedDetailsButton data={data} label="查看角色" />
        </div>
      </BaseNodeContent>
    </NodeCard>
  );
}

function CharacterCover({ src, name }: { src: string; name: string }) {
  return <NodeImage src={src} alt={name} className="absolute inset-0 size-full object-cover" />;
}

function CharacterExtraImages({
  extras,
}: {
  extras: NonNullable<CanvasNodeData["character"]>["extras"];
}) {
  return extras.map((extra) => (
    <span key={extra.assetId} className="bg-muted size-9 shrink-0 overflow-hidden rounded-[10px]">
      <NodeImage src={extra.src} alt="" compact className="size-full object-cover" />
    </span>
  ));
}

function EditableCharacterNode({
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
  const afterImage = useCollaborativeVoiceAfterImage(id, data, voiceGen);
  const voicePrompt = data.character?.voicePrompt ?? "";
  const voiceBlocked = afterImage.pending
    ? "出图后会接着生成音色"
    : voiceGen.blockedReason(voicePrompt);
  useVoiceAfterImage(id, data, voiceGen);
  /** 「生成（含音色）」：先提交形象，这次的任务 id 记进 voiceAfterImage，出图后用新主图接着生成音色 */
  const submitWithVoice = () =>
    voiceBlocked
      ? Promise.resolve(voiceBlocked)
      : vm.submit((taskIds) => {
          afterImage.remember(taskIds);
          return { voiceAfterImage: taskIds };
        });
  useRegisterNodeActions(id, {
    blockedReason: () => vm.blockedReason,
    submit: () => vm.submit(),
    setModel: vm.applyModel,
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
  // 主图是用户自己上传 / 从素材库选的：没有提示词可改，不出生成面板（功能区照常）
  const userImage = characterImageUploaded(data);
  return (
    <>
      <CharacterNodeBody id={id} data={data} vm={vm} voiceGen={voiceGen} />
      <NodeOverlaysPresence multiSelected={multiSelected}>
        {selected && !dragSelected && (
          <NodeOverlays key="overlays" id={id} data={data}>
            {userImage
              ? undefined
              : (width) => (
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
      {!userImage && (
        <PromptEditDialog
          id={id}
          title={data.label}
          actions={
            <button
              type="button"
              className={PROMPT_DIALOG_ACTION_CLASS}
              onClick={() => {
                usePromptEditor.getState().close();
                useCharacterDetail.getState().open(id);
              }}
            >
              角色详情与音色
            </button>
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
      )}
    </>
  );
}

/** 声音格里的音频试听：点一下播放 / 暂停（全站同一时刻只放一条）。绑了有名字的音色时，格子里直接写出名字。 */
function AudioCell({ asset, name }: { asset: AssetDto; name?: string }) {
  const playing = useVoicePlaying(asset.id);
  const label = name?.trim() ?? "";
  return (
    <button
      type="button"
      title={label ? `${label}（点击试听 / 暂停）` : "点击试听 / 暂停"}
      aria-label={
        playing ? `暂停试听${label ? `：${label}` : ""}` : `试听声音${label ? `：${label}` : ""}`
      }
      aria-pressed={playing}
      onClick={(event) => {
        event.stopPropagation();
        toggleVoice(asset);
      }}
      onPointerDown={(event) => event.stopPropagation()}
      className={cn(
        "focus-visible:ring-ring/60 bg-info/15 text-info flex h-9 min-w-9 shrink-0 items-center gap-1.5 rounded-[10px] px-2 outline-none focus-visible:ring-2 [&_svg]:size-4 [&_svg]:shrink-0",
        !label && "w-9 justify-center px-0",
      )}
    >
      {playing ? <Pause /> : <Play />}
      {label && <span className="max-w-24 truncate text-[11px] leading-none">{label}</span>}
    </button>
  );
}

/**
 * 节点正文：封面（空 / 生成中 / 失败 / 主图）、声音格 + 补充图格、形象名与状态标签、「…」菜单入口。
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

  // 声音任务的结果由这里回填（节点始终挂载，详情页关着也能收到）
  const voiceTask = useVoiceBackfill(id, data);
  const cell = voiceCell(character, voiceTask.phase === "generating");

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
    selected: vm.paramFrameAspect,
  });

  // 生成过多张（或换过主图）：右上角出「历史」徽标，点开选一张当主图，和图片节点一样
  const badge = showVersionBadge(data);

  const showTask = SHOWS_TASK_BODY.has(vm.view.phase);
  // 主图在传：节点上自己点的，或者从短剧资产列表 / 拖放传进来的（只记在节点的 uploadProgress 上）
  const mainUploading = media.uploading === "main" || data.uploadProgress !== undefined;
  const extraFull = character.extras.length >= MAX_EXTRAS;

  return (
    <NodeCard
      title={data.label}
      onRename={(label) => updateNodeData(id, { label })}
      icon={KindIcon ? <KindIcon /> : undefined}
      status={showTask ? <NodeStatusLabel view={vm.view} /> : undefined}
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
            <CharacterCover src={data.src} name={character.name} />
          ) : (
            <div className="bg-muted absolute inset-0 flex flex-col items-center justify-center gap-2 px-6">
              <Button
                variant="secondary"
                size="sm"
                className="w-full max-w-40"
                disabled={mainUploading}
                onClick={() => media.pickFile("image", "main")}
              >
                <Upload />
                {mainUploading ? "上传中…" : "上传"}
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
              name={character.voice.name}
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
          <CharacterExtraImages extras={character.extras} />
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
