import { useEffect, useState } from "react";
import { useReactFlow } from "@xyflow/react";
import { AudioLines, FolderOpen, Sparkles, Upload, X } from "lucide-react";
import { toast } from "sonner";

import type { AssetDto } from "@/api/asset/type";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { useCharacterMedia } from "@/hooks/use-character-media";
import { cn } from "@/lib/utils";
import type { CanvasNode, CanvasNodeData } from "@/types";
import {
  applyPickedAssets,
  emptyCharacter,
  MAX_EXTRAS,
  MAX_NAME,
  MAX_TEXT,
  renameCharacter,
  roleNames,
  setVoiceMode,
} from "@/utils/characters/node-character";

import { LibraryPickerForCharacter } from "./character-library-picker";

/** 详情里的小标题 */
const LABEL = "text-[13px] font-medium";

/** 音频音色的试听行：播放键 + 文件来源说明 */
function AudioRow({ assetId, src, origin }: { assetId: string; src: string; origin: string }) {
  const playing = useVoicePlaying(assetId);
  const label = { upload: "上传的音频", generated: "AI 生成的音频", library: "素材库里的音频" }[
    origin
  ];
  return (
    <button
      type="button"
      onClick={() => toggleVoice({ id: assetId, url: src } as AssetDto)}
      aria-pressed={playing}
      className="bg-muted hover:bg-accent focus-visible:ring-ring/50 flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm outline-none focus-visible:ring-2"
    >
      <AudioLines className="text-info size-4" />
      <span className="flex-1">{label}</span>
      <span className="text-muted-foreground text-xs">
        {playing ? "播放中，点击暂停" : "点击试听"}
      </span>
    </button>
  );
}

/**
 * 角色卡详情（设计 §6.1）：左边大图 + 补充图（可移除）+「上传源素材」，右边角色名称（可选画布里的已有角色）、
 * 形象名称、出现集数（短剧画布，只读，改集数用节点上方的功能条）、形象音色。
 * 所有修改直接写回节点，没有「保存」按钮：连着打字合并成一步撤销，和画布里改节点是同一套。
 */
export function CharacterDetailDialog({
  id,
  data,
  open,
  onOpenChange,
}: {
  id: string;
  data: CanvasNodeData;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { updateNodeData, getNodes } = useReactFlow<CanvasNode>();
  const character = data.character ?? emptyCharacter(data.label);
  /** 角色名输入框：允许暂时清空，失焦时空名恢复原名；有内容就实时写回 */
  const [name, setName] = useState(character.name);
  const [text, setText] = useState(character.voice?.mode === "text" ? character.voice.text : "");
  /** 切到文本音色前绑过的音频：切回音频时沿用，不让用户重新选 */
  const [keptAudio, setKeptAudio] = useState(
    character.voice?.mode === "audio" ? character.voice : undefined,
  );
  const mode: "text" | "audio" = character.voice?.mode === "audio" ? "audio" : "text";

  // 打开时以节点当前数据为准（节点在别处被改过、撤销过都要跟上）
  useEffect(() => {
    if (!open) return;
    setName(character.name);
    setText(character.voice?.mode === "text" ? character.voice.text : "");
    setKeptAudio(character.voice?.mode === "audio" ? character.voice : undefined);
    // 只在打开的那一刻同步；之后的输入由本组件自己持有
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const apply = (
    assets: AssetDto[],
    purpose: "main" | "extra" | "voice",
    origin: "upload" | "library" = "library",
  ) => {
    const { patch, warnings } = applyPickedAssets(data, assets, purpose, origin);
    if (Object.keys(patch).length > 0) updateNodeData(id, patch);
    if (purpose === "voice" && patch.character?.voice?.mode === "audio") {
      setKeptAudio(patch.character.voice);
    }
    if (warnings.length > 0) toast.warning([...new Set(warnings)].join("；"));
  };
  const media = useCharacterMedia((asset, purpose) => apply([asset], purpose, "upload"));

  const names = roleNames(getNodes().map((node) => ({ data: node.data })));
  const episodes = data.drama?.episodes;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[calc(100dvh-4rem)] gap-0 overflow-y-auto p-0 sm:max-w-3xl">
        <DialogHeader className="sr-only">
          <DialogTitle>角色卡详情</DialogTitle>
          <DialogDescription>修改角色名称、形象名称、补充图和音色</DialogDescription>
        </DialogHeader>
        <div className="grid gap-0 sm:grid-cols-[18rem_minmax(0,1fr)]">
          <div className="flex flex-col gap-3 p-4">
            <div className="bg-muted relative aspect-3/4 w-full overflow-hidden rounded-xl">
              {data.src ? (
                <img
                  src={data.src}
                  alt={character.name}
                  draggable={false}
                  className="size-full object-cover"
                />
              ) : (
                <div className="text-muted-foreground grid size-full place-items-center px-6 text-center text-sm">
                  还没有主图
                  <br />
                  可以上传、从素材库选，或回到节点下方直接生成
                </div>
              )}
            </div>
            <div className="flex flex-wrap gap-2">
              {character.extras.map((extra, index) => (
                <span
                  key={extra.assetId}
                  className="bg-muted relative h-[58px] w-11 overflow-hidden rounded-lg"
                >
                  <img
                    src={extra.src}
                    alt=""
                    draggable={false}
                    className="size-full object-cover"
                  />
                  <button
                    type="button"
                    aria-label="移除补充图"
                    onClick={() =>
                      updateNodeData(id, {
                        character: {
                          ...character,
                          extras: character.extras.filter((_, i) => i !== index),
                        },
                      })
                    }
                    className="absolute top-0.5 right-0.5 grid size-4 place-items-center rounded-full bg-black/60 text-white"
                  >
                    <X className="size-2.5" />
                  </button>
                </span>
              ))}
              <span className="text-muted-foreground self-end text-xs">
                补充图 {character.extras.length}/{MAX_EXTRAS}
              </span>
            </div>
            <div className="flex gap-2">
              <Button
                variant="outline"
                className="flex-1"
                disabled={media.uploading === "extra" || character.extras.length >= MAX_EXTRAS}
                title={
                  character.extras.length >= MAX_EXTRAS ? `补充图最多 ${MAX_EXTRAS} 张` : undefined
                }
                onClick={() => media.pickFile("image", data.src ? "extra" : "main")}
              >
                <Upload />
                上传源素材
              </Button>
              <Button
                variant="outline"
                className="flex-1"
                disabled
                title="拼图的含义还没有确定，一期不做"
              >
                拼图
              </Button>
            </div>
          </div>

          <div className="border-border flex flex-col gap-4 border-t p-5 sm:border-t-0 sm:border-l">
            <div className="flex flex-col gap-1.5">
              <label className={LABEL} htmlFor={`${id}-name`}>
                角色名称
              </label>
              <div className="flex gap-2">
                <Input
                  id={`${id}-name`}
                  value={name}
                  maxLength={MAX_NAME}
                  onChange={(event) => {
                    setName(event.target.value);
                    const next = event.target.value.trim();
                    if (next) updateNodeData(id, renameCharacter(data, next));
                  }}
                  onBlur={() => {
                    if (!name.trim()) setName(character.name);
                  }}
                />
                <Select
                  value=""
                  onValueChange={(value) => {
                    if (!value) return;
                    setName(value);
                    updateNodeData(id, renameCharacter(data, value));
                  }}
                >
                  <SelectTrigger className="w-32 shrink-0" aria-label="选择画布里已有的角色名">
                    <SelectValue placeholder="已有角色" />
                  </SelectTrigger>
                  <SelectContent>
                    {names.length === 0 && (
                      <div className="text-muted-foreground px-2 py-1.5 text-sm">没有已有角色</div>
                    )}
                    {names.map((item) => (
                      <SelectItem key={item} value={item}>
                        {item}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <p className="text-muted-foreground text-xs">
                同一个角色的不同形象，用相同的角色名归在一起；只改当前这个节点。
              </p>
            </div>

            <div className="flex flex-col gap-1.5">
              <label className={LABEL} htmlFor={`${id}-look`}>
                形象名称
              </label>
              <Input
                id={`${id}-look`}
                value={character.look}
                maxLength={MAX_NAME}
                onChange={(event) =>
                  updateNodeData(id, { character: { ...character, look: event.target.value } })
                }
              />
            </div>

            {episodes && (
              <div className="flex flex-col gap-1.5">
                <span className={LABEL}>出现集数</span>
                <p className="bg-muted rounded-lg px-3 py-2 text-sm">
                  {episodes.length === 0 ? "全集" : `第 ${episodes.join("、")} 集`}
                  <span className="text-muted-foreground ml-2 text-xs">
                    选中节点后用上方功能条修改
                  </span>
                </p>
              </div>
            )}

            <div className="flex flex-col gap-2">
              <span className={LABEL}>形象音色</span>
              <div role="group" aria-label="音色类型" className="bg-muted flex rounded-lg p-0.5">
                {(
                  [
                    ["text", "文本音色"],
                    ["audio", "音频音色"],
                  ] as const
                ).map(([key, label]) => (
                  <button
                    key={key}
                    type="button"
                    aria-pressed={mode === key}
                    onClick={() =>
                      updateNodeData(id, {
                        character: setVoiceMode(character, key, text, keptAudio),
                      })
                    }
                    className={cn(
                      "focus-visible:ring-ring/50 h-8 flex-1 rounded-md text-sm outline-none focus-visible:ring-2",
                      mode === key
                        ? "bg-background font-medium shadow-sm"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    {label}
                  </button>
                ))}
              </div>
              {mode === "text" ? (
                <>
                  <Textarea
                    value={text}
                    maxLength={MAX_TEXT}
                    placeholder="请输入音色描述，例如：年轻女声，偏低，语速慢，句尾轻"
                    className="min-h-24"
                    onChange={(event) => {
                      setText(event.target.value);
                      updateNodeData(id, {
                        character: setVoiceMode(character, "text", event.target.value),
                      });
                    }}
                  />
                  <p className="text-muted-foreground text-xs">
                    文本音色只作展示，不会当作参考音频带入视频生成。
                  </p>
                </>
              ) : (
                <>
                  {character.voice?.mode === "audio" ? (
                    <AudioRow
                      assetId={character.voice.assetId}
                      src={character.voice.src}
                      origin={character.voice.origin}
                    />
                  ) : (
                    <p className="text-muted-foreground bg-muted rounded-lg px-3 py-2 text-sm">
                      还没有音频，请上传、绑定已有音频
                    </p>
                  )}
                  <div className="grid grid-cols-3 gap-2">
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={media.uploading === "voice"}
                      onClick={() => media.pickFile("audio", "voice")}
                    >
                      <Upload />
                      {media.uploading === "voice" ? "上传中…" : "上传音频"}
                    </Button>
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled
                      title="AI 生成音色还没有接入（音频生成任务怎么提交待确认），先上传或绑定已有音频"
                    >
                      <Sparkles />
                      AI 生成
                    </Button>
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => media.openLibrary("audio", "voice")}
                    >
                      <FolderOpen />
                      绑定已有
                    </Button>
                  </div>
                </>
              )}
            </div>
          </div>
        </div>
        {/* 放在详情弹框里面：嵌套弹框才能共用焦点与 Esc 的层级，Esc 先关选素材这层 */}
        <LibraryPickerForCharacter
          request={media.library}
          data={data}
          onClose={media.closeLibrary}
          onConfirm={(assets, purpose) => apply(assets, purpose, "library")}
        />
      </DialogContent>
    </Dialog>
  );
}
