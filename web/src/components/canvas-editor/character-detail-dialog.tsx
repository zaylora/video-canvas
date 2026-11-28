import { Fragment, useEffect, useState } from "react";
import { useReactFlow } from "@xyflow/react";
import {
  AudioLines,
  FolderOpen,
  Loader2,
  Pause,
  Play,
  Plus,
  Upload,
  UserRound,
  UsersRound,
  X,
} from "lucide-react";
import { toast } from "sonner";

import { getAsset } from "@/api/asset";
import type { AssetDto } from "@/api/asset/type";
import { ChromeButton } from "@/components/canvas/chrome/chrome";
import { MediaLightbox, type LightboxTarget } from "@/components/canvas/media-lightbox";
import { ModelMenu } from "@/components/canvas/model-menu";
import { PANEL_CHIP_CLASS } from "@/components/canvas/node-prompt-input";
import { toggleVoice, useVoicePlaying } from "@/components/characters/voice-player";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { useCharacterMedia } from "@/hooks/use-character-media";
import type { VoiceGeneration } from "@/hooks/use-voice-generation";
import { useVoiceHistory } from "@/hooks/use-voice-history";
import { cn } from "@/lib/utils";
import type { CanvasNode, CanvasNodeData } from "@/types";
import type { PreviewItem } from "@/utils/canvas/preview-items";
import {
  applyHumanImages,
  applyPickedAssets,
  emptyCharacter,
  MAX_EXTRAS,
  MAX_NAME,
  MAX_TEXT,
  renameCharacter,
} from "@/utils/characters/node-character";
import { stripVoicePrefix } from "@/utils/assets/official-voices";
import { setVoiceFromHistory } from "@/utils/characters/voice-history";
import { episodesLabel } from "@/utils/drama/assets";

import { HumanPickerDialog } from "@/components/virtual-human/human-picker-dialog";

import { LibraryPickerForCharacter } from "./character-library-picker";
import { VoiceHistoryList } from "./voice-history-list";

/**
 * 弹框外壳：半透明清透面板（coding-standards 6.1，同资产库弹框），覆盖 DialogContent 的实心底和小圆角；
 * 限高，左右两栏各自滚动（窄屏叠成一栏整体滚动）
 */
const CONTENT_CLASS =
  "bg-chrome ring-chrome-border flex max-h-[min(88svh,760px)] flex-col gap-0 overflow-hidden rounded-2xl p-0 [corner-shape:squircle] shadow-2xl backdrop-blur-2xl backdrop-saturate-150 sm:max-w-[52rem]";

/** 灯箱里主图那一项的 id（补充图用各自的素材 id） */
const MAIN_ZOOM_ID = "main";

/** 分区小标题 */
const LABEL = "text-muted-foreground text-xs font-medium";

/** 面板里的输入框：半透明胶囊（coding-standards 6.1），覆盖默认实心输入框和深色底 */
const FIELD_CLASS =
  "bg-chrome-hover dark:bg-chrome-hover border-chrome-border h-10 rounded-full px-4 text-sm";

/** 分段胶囊里的一段（同 ChromeSegment） */
const SEGMENT_CLASS =
  "hover:bg-chrome-hover text-foreground focus-visible:ring-ring/60 inline-flex h-8 min-w-0 flex-1 items-center justify-center gap-1.5 rounded-full px-3 text-xs whitespace-nowrap transition-colors duration-150 outline-none focus-visible:ring-2 disabled:pointer-events-none disabled:opacity-40 [&_svg]:size-3.5 [&_svg]:shrink-0";

type Segment = {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  disabled?: boolean;
};

/** 半透明分段胶囊：两三个并列的操作（上传 | 素材库），段间细分隔线 */
function SegmentedPill({ segments, className }: { segments: Segment[]; className?: string }) {
  return (
    <div
      className={cn(
        "bg-chrome ring-chrome-border flex items-center gap-0.5 rounded-full p-1 shadow-sm ring-1 backdrop-blur-xl backdrop-saturate-150",
        className,
      )}
    >
      {segments.map((segment, index) => (
        <Fragment key={segment.label}>
          {index > 0 && <span aria-hidden className="bg-chrome-border h-4 w-px shrink-0" />}
          <button
            type="button"
            className={SEGMENT_CLASS}
            disabled={segment.disabled}
            onClick={segment.onClick}
          >
            {segment.icon}
            {segment.label}
          </button>
        </Fragment>
      ))}
    </div>
  );
}

/** 声音来源的说法 */
const ORIGIN_LABEL: Record<string, string> = {
  upload: "上传",
  generated: "AI 生成",
  library: "素材库",
};

/** 试听条里的几根声波：播放时跳动，暂停时静止 */
const BARS = [0.45, 0.8, 0.55, 1, 0.65, 0.9, 0.4, 0.7, 0.5];

/** 当前声音的试听条：圆形播放键 + 声波 + 来源 */
function VoicePlayer({
  assetId,
  src,
  origin,
  name,
}: {
  assetId: string;
  src: string;
  origin: string;
  name?: string;
}) {
  const playing = useVoicePlaying(assetId);
  return (
    <div className="flex h-12 items-center gap-3 rounded-full pr-4 pl-1.5">
      <ChromeButton
        variant="primary"
        aria-label={playing ? "暂停" : "试听"}
        aria-pressed={playing}
        onClick={() => toggleVoice({ id: assetId, url: src } as AssetDto)}
      >
        {playing ? <Pause /> : <Play className="translate-x-px" />}
      </ChromeButton>
      <span aria-hidden className="flex h-5 items-center gap-[3px]">
        {BARS.map((height, index) => (
          <i
            key={index}
            className={cn(
              "bg-foreground/70 w-[3px] rounded-full motion-reduce:animate-none",
              playing && "animate-pulse",
            )}
            style={{ height: `${height * 100}%`, animationDelay: `${index * 90}ms` }}
          />
        ))}
      </span>
      <span className="text-muted-foreground ml-auto min-w-0 truncate text-right text-xs">
        {name?.trim() || ORIGIN_LABEL[origin] || "音频"}
      </span>
    </div>
  );
}

/**
 * 角色卡详情（设计 §6.1）：左边大图 + 补充图（可移除）+「上传源素材」，右边角色名称、
 * 形象名称、出现集数（短剧画布，只读，改集数用节点上方的功能条）、声音（上传 / 绑定已有 / AI 生成）。
 * 所有修改直接写回节点，没有「保存」按钮：连着打字合并成一步撤销，和画布里改节点是同一套。
 */
export function CharacterDetailDialog({
  id,
  data,
  voiceGen,
  open,
  onOpenChange,
}: {
  id: string;
  data: CanvasNodeData;
  /** 声音生成（节点持有的同一份，短剧列表的批量生成也用它） */
  voiceGen: VoiceGeneration;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { updateNodeData, getNode } = useReactFlow<CanvasNode>();
  const character = data.character ?? emptyCharacter(data.label);
  /** 角色名输入框：允许暂时清空，失焦时空名恢复原名；有内容就实时写回 */
  const [name, setName] = useState(character.name);
  // 打开时以节点当前数据为准（节点在别处被改过、撤销过都要跟上）
  useEffect(() => {
    if (!open) return;
    setName(character.name);
    // 只在打开的那一刻同步；之后的输入由本组件自己持有
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const apply = (
    assets: AssetDto[],
    purpose: "main" | "extra" | "voice",
    origin: "upload" | "library" = "library",
  ) => {
    const current = getNode(id)?.data;
    if (!current) return;
    const { patch, warnings } = applyPickedAssets(current, assets, purpose, origin);
    if (Object.keys(patch).length > 0) updateNodeData(id, patch);
    if (warnings.length > 0) toast.warning([...new Set(warnings)].join("；"));
  };
  const media = useCharacterMedia((asset, purpose) => apply([asset], purpose, "upload"));

  // 早年绑的音色没记名字（详情页此前只显示「素材库」）：打开时补查一次素材名，写回节点。
  // 查不到（素材被清理、没网）就算了，保持原样，不影响试听与生成。
  const voiceAssetId = character.voice?.assetId;
  const voiceName = character.voice?.name;
  useEffect(() => {
    if (!open || !voiceAssetId || voiceName?.trim()) return;
    let alive = true;
    getAsset(voiceAssetId)
      .then((asset) => {
        const name = stripVoicePrefix(asset.fileName);
        const cur = getNode(id)?.data.character;
        if (!alive || !name || !cur || cur.voice?.assetId !== voiceAssetId) return;
        if (cur.voice.name?.trim()) return;
        updateNodeData(id, { character: { ...cur, voice: { ...cur.voice, name } } });
      })
      .catch(() => {
        // 静默失败：名字只是展示用
      });
    return () => {
      alive = false;
    };
  }, [open, voiceAssetId, voiceName, id, getNode, updateNodeData]);
  /** 从真人库引入：真人的可用图片替换主图和补充图，之后照常可以换、删 */
  const [pickingHuman, setPickingHuman] = useState(false);
  const applyHuman = (images: AssetDto[]) => {
    const current = getNode(id)?.data;
    if (!current) return;
    const patch = applyHumanImages(current, images);
    if (Object.keys(patch).length > 0) updateNodeData(id, patch);
  };

  const episodes = data.drama?.episodes;
  const history = useVoiceHistory(
    id,
    open,
    data.voiceTaskId,
    character.voice?.assetId,
    voiceGen.models,
  );
  /** 放大预览：主图和补充图排成一组，点哪张从哪张开始，从被点的图的位置放大出来 */
  const [zoom, setZoom] = useState<LightboxTarget | null>(null);
  const zoomItems: PreviewItem[] = [
    ...(data.src
      ? [{ id: MAIN_ZOOM_ID, src: data.src, mediaType: "image" as const, label: character.name }]
      : []),
    ...character.extras.map((extra, index) => ({
      id: extra.assetId,
      src: extra.src,
      mediaType: "image" as const,
      label: `${character.name} 补充图 ${index + 1}`,
    })),
  ];
  const openZoom = (zoomId: string, event: React.MouseEvent<HTMLElement>) => {
    const rect = event.currentTarget.getBoundingClientRect();
    setZoom({
      id: zoomId,
      origin: { x: rect.left + rect.width / 2, y: rect.top + rect.height / 2 },
    });
  };
  /** 把一条生成记录设为音色：和上传、绑定一样直接写回节点，可以撤销 */
  const applyHistoryVoice = (asset: { id: string; url: string }) => {
    const patch = setVoiceFromHistory(data, asset);
    if (patch) updateNodeData(id, patch);
  };

  const generating = voiceGen.task.phase === "generating";
  const extrasFull = character.extras.length >= MAX_EXTRAS;
  const removeExtra = (index: number) =>
    updateNodeData(id, {
      character: { ...character, extras: character.extras.filter((_, i) => i !== index) },
    });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        // 弹框渲染在节点组件里，React 事件会沿组件树冒泡到画布：不拦的话弹框里的双击会被当成双击空白画布，弹出「添加节点」菜单；
        // 单击也拦下，从短剧资产列表打开时免得在列表后面把节点选中
        onClick={(event) => event.stopPropagation()}
        onDoubleClick={(event) => event.stopPropagation()}
        className={CONTENT_CLASS}
      >
        <DialogHeader className="sr-only">
          <DialogTitle>角色卡详情</DialogTitle>
          <DialogDescription>修改角色名称、形象名称、补充图和音色</DialogDescription>
        </DialogHeader>
        <div className="grid min-h-0 flex-1 overflow-y-auto sm:grid-cols-[19rem_minmax(0,1fr)] sm:overflow-hidden">
          {/* 左：形象舞台。主图完整显示（三视图不裁切），下面固定四格补充图 */}
          <div className="flex flex-col gap-4 p-5 sm:overflow-y-auto">
            <div className="bg-chrome-hover ring-chrome-border relative aspect-3/4 w-full overflow-hidden rounded-2xl ring-1">
              {data.src ? (
                <button
                  type="button"
                  aria-label="放大预览形象图"
                  title="放大预览"
                  onClick={(event) => openZoom(MAIN_ZOOM_ID, event)}
                  className="focus-visible:ring-ring/60 size-full cursor-zoom-in outline-none focus-visible:ring-2 focus-visible:ring-inset"
                >
                  <img
                    src={data.src}
                    alt={character.name}
                    draggable={false}
                    className="size-full object-contain"
                  />
                </button>
              ) : (
                <div className="text-muted-foreground flex size-full flex-col items-center justify-center gap-2.5 px-6 text-sm">
                  {media.uploading === "main" ? (
                    <Loader2 className="size-6 animate-spin" />
                  ) : (
                    <UserRound className="size-7 stroke-[1.5]" />
                  )}
                  {media.uploading === "main" ? "上传中…" : "还没有形象"}
                </div>
              )}
              {!data.src && media.uploading !== "main" && (
                <SegmentedPill
                  className="absolute inset-x-3 bottom-3"
                  segments={[
                    {
                      icon: <Upload />,
                      label: "上传",
                      onClick: () => media.pickFile("image", "main"),
                    },
                    {
                      icon: <FolderOpen />,
                      label: "素材库",
                      onClick: () => media.openLibrary("image", "main"),
                    },
                    {
                      icon: <UsersRound />,
                      label: "真人库",
                      onClick: () => setPickingHuman(true),
                    },
                  ]}
                />
              )}
            </div>

            <div className="flex flex-col gap-2">
              <div className="flex h-8 items-center gap-1">
                <span className={LABEL}>补充图</span>
                <span className="text-muted-foreground/70 text-xs tabular-nums">
                  {character.extras.length}/{MAX_EXTRAS}
                </span>
                <ChromeButton
                  className="ml-auto"
                  aria-label="从真人库引入"
                  title="从真人库引入（替换主图和补充图）"
                  onClick={() => setPickingHuman(true)}
                >
                  <UsersRound />
                </ChromeButton>
                <ChromeButton
                  aria-label="从素材库选补充图"
                  title={
                    !data.src
                      ? "先放一张主图"
                      : extrasFull
                        ? `补充图最多 ${MAX_EXTRAS} 张`
                        : "从素材库选"
                  }
                  disabled={!data.src || extrasFull}
                  onClick={() => media.openLibrary("image", "extra")}
                >
                  <FolderOpen />
                </ChromeButton>
              </div>
              <div className="grid grid-cols-4 gap-2">
                {Array.from({ length: MAX_EXTRAS }, (_, index) => {
                  const extra = character.extras[index];
                  if (extra) {
                    return (
                      <span
                        key={extra.assetId}
                        className="group/extra bg-chrome-hover ring-chrome-border relative aspect-3/4 overflow-hidden rounded-xl ring-1"
                      >
                        <button
                          type="button"
                          aria-label={`放大预览补充图 ${index + 1}`}
                          onClick={(event) => openZoom(extra.assetId, event)}
                          className="focus-visible:ring-ring/60 size-full cursor-zoom-in outline-none focus-visible:ring-2 focus-visible:ring-inset"
                        >
                          <img
                            src={extra.src}
                            alt=""
                            draggable={false}
                            className="size-full object-cover"
                          />
                        </button>
                        <button
                          type="button"
                          aria-label="移除补充图"
                          onClick={() => removeExtra(index)}
                          className="focus-visible:ring-ring/60 absolute top-1 right-1 grid size-5 place-items-center rounded-full bg-black/60 text-white opacity-0 backdrop-blur-sm transition-opacity duration-150 outline-none group-hover/extra:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 [@media(hover:none)]:opacity-100"
                        >
                          <X className="size-3" />
                        </button>
                      </span>
                    );
                  }
                  // 只有第一个空格能点，后面的空格只占位
                  const next = index === character.extras.length;
                  return (
                    <button
                      key={`slot-${index}`}
                      type="button"
                      aria-label="上传补充图"
                      disabled={!next || !data.src || media.uploading === "extra"}
                      title={next && !data.src ? "先放一张主图" : undefined}
                      onClick={() => media.pickFile("image", "extra")}
                      className="border-chrome-border text-muted-foreground hover:bg-chrome-hover hover:text-foreground focus-visible:ring-ring/60 grid aspect-3/4 place-items-center rounded-xl border border-dashed transition-colors duration-150 outline-none focus-visible:ring-2 disabled:pointer-events-none disabled:opacity-40"
                    >
                      {next && media.uploading === "extra" ? (
                        <Loader2 className="size-4 animate-spin" />
                      ) : (
                        next && <Plus className="size-4" />
                      )}
                    </button>
                  );
                })}
              </div>
            </div>
          </div>

          {/* 右：身份 + 声音 */}
          <div className="border-chrome-border flex flex-col gap-6 border-t p-5 pt-6 sm:overflow-y-auto sm:border-t-0 sm:border-l sm:pr-6">
            <div className="flex flex-col gap-3 pr-8">
              <div className="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
                <div className="flex min-w-0 flex-col gap-1.5">
                  <label className={LABEL} htmlFor={`${id}-name`}>
                    角色名称
                  </label>
                  <Input
                    id={`${id}-name`}
                    value={name}
                    maxLength={MAX_NAME}
                    className={FIELD_CLASS}
                    onChange={(event) => {
                      setName(event.target.value);
                      const next = event.target.value.trim();
                      if (next) updateNodeData(id, renameCharacter(data, next));
                    }}
                    onBlur={() => {
                      if (!name.trim()) setName(character.name);
                    }}
                  />
                </div>
                <div className="flex min-w-0 flex-col gap-1.5">
                  <label className={LABEL} htmlFor={`${id}-look`}>
                    形象名称
                  </label>
                  <Input
                    id={`${id}-look`}
                    value={character.look}
                    maxLength={MAX_NAME}
                    className={FIELD_CLASS}
                    onChange={(event) =>
                      updateNodeData(id, { character: { ...character, look: event.target.value } })
                    }
                  />
                </div>
              </div>
              {episodes && (
                <div className="flex items-center gap-2">
                  <span className={LABEL}>出现集数</span>
                  <span className="bg-chrome-hover ring-chrome-border rounded-full px-2.5 text-xs leading-6 ring-1">
                    {episodesLabel(episodes)}
                  </span>
                </div>
              )}
            </div>

            <section className="bg-chrome-hover ring-chrome-border flex flex-col gap-4 rounded-xl p-4 ring-1 [corner-shape:squircle]">
              <div className="flex items-center gap-2">
                <AudioLines className="text-muted-foreground size-4" />
                <h3 className="text-sm font-semibold">声音</h3>
                <SegmentedPill
                  className="ml-auto"
                  segments={[
                    {
                      icon:
                        media.uploading === "voice" ? (
                          <Loader2 className="animate-spin" />
                        ) : (
                          <Upload />
                        ),
                      label: media.uploading === "voice" ? "上传中" : "上传",
                      disabled: media.uploading === "voice",
                      onClick: () => media.pickFile("audio", "voice"),
                    },
                    {
                      icon: <FolderOpen />,
                      label: "绑定已有",
                      onClick: () => media.openLibrary("audio", "voice"),
                    },
                  ]}
                />
              </div>

              {/* 当前声音 + 历史版本：生成记录就是这个声音的各个版本，和当前声音放在一起，一直展开、列表限高 */}
              <div className="bg-chrome ring-chrome-border overflow-hidden rounded-2xl ring-1 [corner-shape:squircle]">
                <div className="flex items-center gap-1 pr-1.5">
                  <div className="min-w-0 flex-1">
                    {character.voice ? (
                      <VoicePlayer
                        assetId={character.voice.assetId}
                        src={character.voice.src}
                        origin={character.voice.origin}
                        name={character.voice.name}
                      />
                    ) : (
                      <p className="text-muted-foreground flex h-12 items-center px-5 text-sm">
                        还没有声音
                      </p>
                    )}
                  </div>
                </div>
                {history.length > 0 && (
                  <div className="border-chrome-border border-t">
                    <VoiceHistoryList rows={history} onUse={applyHistoryVoice} />
                  </div>
                )}
              </div>

              {/* 生成新声音：输入框和底栏是一整块，模型和生成按钮收在底部 */}
              <div className="flex flex-col gap-2">
                <label className={LABEL} htmlFor={`${id}-voice-prompt`}>
                  AI 生成
                </label>
                <div className="bg-chrome ring-chrome-border focus-within:ring-ring/50 flex flex-col rounded-2xl ring-1 transition-shadow duration-150 [corner-shape:squircle]">
                  <Textarea
                    id={`${id}-voice-prompt`}
                    value={character.voicePrompt ?? ""}
                    maxLength={MAX_TEXT}
                    disabled={generating}
                    placeholder="音色描述加一句试音台词，例如：年轻女声，偏低，语速慢，句尾轻：“我回来了。”"
                    className="max-h-36 min-h-16 resize-none overflow-y-auto border-0 bg-transparent px-4 pt-3 pb-1 shadow-none focus-visible:ring-0 disabled:bg-transparent dark:bg-transparent dark:disabled:bg-transparent"
                    onChange={(event) =>
                      updateNodeData(id, {
                        character: { ...character, voicePrompt: event.target.value },
                      })
                    }
                  />
                  <div className="flex items-center gap-2 p-2 pl-2.5">
                    <ModelMenu
                      models={voiceGen.options}
                      model={voiceGen.options.find((item) => item.id === voiceGen.model?.key)}
                      onModelChange={voiceGen.selectModel}
                      placeholder={voiceGen.modelsStatus === "loading" ? "加载中…" : "没有音频模型"}
                      disabled={generating || voiceGen.options.length === 0}
                      className={cn(PANEL_CHIP_CLASS, "mr-auto")}
                    />
                    {generating ? (
                      <Button
                        variant="outline"
                        className="h-9 rounded-full px-4"
                        onClick={() => void voiceGen.cancel()}
                      >
                        <Loader2 className="animate-spin" />
                        {voiceGen.task.phase === "generating" && voiceGen.task.progress !== null
                          ? `${voiceGen.task.progress}% · 取消`
                          : "取消"}
                      </Button>
                    ) : (
                      <Button
                        className="h-9 rounded-full px-5"
                        disabled={
                          !!voiceGen.unavailable ||
                          voiceGen.submitting ||
                          !voiceGen.model ||
                          !!data.voiceAfterImage?.length
                        }
                        title={
                          voiceGen.unavailable ??
                          (data.voiceAfterImage?.length ? "出图后会接着生成音色" : undefined)
                        }
                        onClick={() => void voiceGen.submit(character.voicePrompt ?? "")}
                      >
                        {voiceGen.task.phase === "failed"
                          ? "重试"
                          : character.voice
                            ? "重新生成"
                            : "生成声音"}
                      </Button>
                    )}
                  </div>
                </div>
                {(voiceGen.error || voiceGen.task.phase === "failed") && (
                  <p className="text-destructive px-1 text-xs" role="alert">
                    {voiceGen.error ??
                      (voiceGen.task.phase === "failed" ? voiceGen.task.reason : null)}
                  </p>
                )}
              </div>
            </section>
          </div>
        </div>
        {/* 放大预览：复用画布的灯箱，主图 + 补充图一起翻看；放在详情弹框里面，Esc 先关预览 */}
        <MediaLightbox
          items={zoomItems}
          target={open ? zoom : null}
          onActiveChange={(next) => setZoom({ id: next, origin: null })}
          countLabel="形象图"
          onClose={() => setZoom(null)}
        />
        {/* 放在详情弹框里面：嵌套弹框才能共用焦点与 Esc 的层级，Esc 先关选素材这层 */}
        <LibraryPickerForCharacter
          request={media.library}
          data={data}
          onClose={media.closeLibrary}
          onConfirm={(assets, purpose) => apply(assets, purpose, "library")}
        />
        <HumanPickerDialog
          open={pickingHuman}
          onOpenChange={setPickingHuman}
          onConfirm={applyHuman}
        />
      </DialogContent>
    </Dialog>
  );
}
