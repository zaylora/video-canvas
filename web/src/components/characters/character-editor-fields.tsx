import { ImagePlus, Replace, Star, X, AudioLines, Plus } from "lucide-react";

import type { AssetDto } from "@/api/asset/type";
import { AssetCover } from "@/components/assets/asset-card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import {
  DESC_MAX,
  EXTRA_MAX,
  type EditorErrors,
  type EditorForm,
  type PickMode,
} from "@/utils/characters/editor-form";
import { formatMediaDuration } from "@/utils/canvas/media-lod";

import { VoicePlayButton, VoiceWave, useVoicePlaying } from "./voice-player";

/** 槽位上悬停才出现的操作条；触屏常显 */
const ACTIONS =
  "absolute inset-x-0 bottom-0 flex justify-center gap-1 bg-gradient-to-t from-cover-scrim to-transparent p-1.5 pt-6 opacity-0 transition-opacity duration-150 group-hover/slot:opacity-100 group-focus-within/slot:opacity-100 max-md:opacity-100 motion-reduce:transition-none";

/** 操作条上的小按钮 */
const ACTION_BUTTON =
  "bg-cover-scrim text-cover-foreground hover:bg-black/80 focus-visible:ring-cover-foreground/70 inline-flex h-6 items-center gap-1 rounded-md px-1.5 text-[11px] outline-none focus-visible:ring-2 disabled:opacity-50 [&_svg]:size-3.5";

/** 左列：主图槽 + 补充图槽 */
function ImageColumn({
  form,
  errors,
  disabled,
  onPick,
  onSetMain,
  onRemoveExtra,
}: {
  form: EditorForm;
  errors: EditorErrors;
  disabled: boolean;
  onPick: (mode: PickMode) => void;
  onSetMain: (index: number) => void;
  onRemoveExtra: (index: number) => void;
}) {
  return (
    <div className="flex w-full shrink-0 flex-col gap-3 sm:w-52">
      <div>
        <div className="mb-1.5 text-[13px] font-medium">
          形象主图 <span className="text-destructive">*</span>
        </div>
        {form.avatar ? (
          <div
            data-slot="avatar-slot"
            className="group/slot bg-muted relative aspect-3/4 overflow-hidden rounded-xl"
          >
            <AssetCover asset={form.avatar} />
            <span className="bg-cover-scrim text-cover-foreground absolute top-2 left-2 rounded-md px-1.5 py-0.5 text-[11px]">
              主图
            </span>
            <div className={ACTIONS}>
              <button
                type="button"
                disabled={disabled}
                onClick={() => onPick("avatar")}
                className={ACTION_BUTTON}
              >
                <Replace />
                更换
              </button>
            </div>
          </div>
        ) : (
          <button
            type="button"
            data-slot="avatar-slot"
            aria-invalid={!!errors.avatar}
            disabled={disabled}
            onClick={() => onPick("avatar")}
            className={cn(
              "text-muted-foreground hover:bg-muted/60 focus-visible:ring-ring/50 flex aspect-3/4 w-full flex-col items-center justify-center gap-1.5 rounded-xl border border-dashed text-[13px] transition-colors outline-none focus-visible:ring-3 disabled:opacity-50",
              errors.avatar && "border-destructive text-destructive bg-destructive/5",
            )}
          >
            <ImagePlus className="size-6" />
            选择主图
            <small className="text-[11px] opacity-70">从资产库选一张人物图</small>
          </button>
        )}
        {errors.avatar && (
          <p role="alert" className="text-destructive mt-1.5 text-xs">
            {errors.avatar}
          </p>
        )}
      </div>

      <div>
        <div className="mb-1.5 text-[13px] font-medium">
          补充图{" "}
          <span className="text-muted-foreground text-xs font-normal tabular-nums">
            {form.extras.length}/{EXTRA_MAX} · 三视图、侧脸等
          </span>
        </div>
        <div className="grid grid-cols-4 gap-1.5">
          {Array.from({ length: EXTRA_MAX }, (_, index) => {
            const asset = form.extras[index];
            return asset ? (
              <div
                key={asset.id}
                data-slot="extra-slot"
                title={asset.fileName ?? undefined}
                className="group/slot bg-muted relative aspect-square overflow-hidden rounded-lg"
              >
                <AssetCover asset={asset} />
                <div className={cn(ACTIONS, "p-1 pt-5")}>
                  <button
                    type="button"
                    aria-label="设为主图"
                    title="设为主图"
                    disabled={disabled}
                    onClick={() => onSetMain(index)}
                    className={cn(ACTION_BUTTON, "px-1")}
                  >
                    <Star />
                  </button>
                  <button
                    type="button"
                    aria-label="移除"
                    title="移除"
                    disabled={disabled}
                    onClick={() => onRemoveExtra(index)}
                    className={cn(ACTION_BUTTON, "px-1")}
                  >
                    <X />
                  </button>
                </div>
              </div>
            ) : (
              <button
                key={`blank-${index}`}
                type="button"
                data-slot="extra-blank"
                aria-label="添加补充图"
                disabled={disabled || index !== form.extras.length}
                onClick={() => onPick("extra")}
                className={cn(
                  "text-muted-foreground hover:bg-muted/60 focus-visible:ring-ring/50 grid aspect-square place-items-center rounded-lg border border-dashed transition-colors outline-none focus-visible:ring-3 disabled:opacity-40",
                )}
              >
                <Plus className="size-4" />
              </button>
            );
          })}
        </div>
      </div>
    </div>
  );
}

/** 声音槽里的试听卡：播放键 + 文件名 + 声波 + 时长 +「更换」+ 移除 */
function VoiceCard({
  voice,
  disabled,
  onPick,
  onClear,
}: {
  voice: AssetDto;
  disabled: boolean;
  onPick: () => void;
  onClear: () => void;
}) {
  const playing = useVoicePlaying(voice.id);
  const duration = formatMediaDuration(voice.durationMs);
  return (
    <div
      data-slot="voice-card"
      className="bg-muted/50 flex items-center gap-2.5 rounded-lg border p-2"
    >
      <VoicePlayButton asset={voice} className="bg-foreground text-background size-8" />
      <div className="min-w-0 flex-1">
        <div className="truncate text-[13px] font-medium">{voice.fileName ?? "音频"}</div>
        <VoiceWave seed={voice.id} bars={40} active={playing} className="text-foreground/70" />
      </div>
      {duration && <span className="text-muted-foreground text-xs tabular-nums">{duration}</span>}
      <Button variant="outline" size="sm" disabled={disabled} onClick={onPick}>
        更换
      </Button>
      <Button
        variant="ghost"
        size="icon-sm"
        aria-label="移除声音"
        disabled={disabled}
        onClick={onClear}
      >
        <X />
      </Button>
    </div>
  );
}

/**
 * 角色卡编辑弹框的表单主体（设计 §6.4）：左图右文。左列主图槽 + 4 个补充图槽（悬停出现「设为主图」「移除」），
 * 右列名称、声音（试听卡片）、角色设定（字数计数）。纯展示：状态与保存都在外层弹框，这里只负责摆出来。
 * @param form 表单内容
 * @param errors 就地错误：名称、设定显示在输入框下，没选主图时主图槽描红
 * @param disabled 保存中：全部控件禁用
 * @param onPick 点槽位去选素材
 * @param onSetMain 把第 index 张补充图设为主图
 * @param onRemoveExtra 移除第 index 张补充图
 * @param onClearVoice 移除声音
 * @param onNameChange 名称输入
 * @param onDescriptionChange 设定输入
 */
export function CharacterEditorFields({
  form,
  errors,
  disabled,
  onPick,
  onSetMain,
  onRemoveExtra,
  onClearVoice,
  onNameChange,
  onDescriptionChange,
}: {
  form: EditorForm;
  errors: EditorErrors;
  disabled: boolean;
  onPick: (mode: PickMode) => void;
  onSetMain: (index: number) => void;
  onRemoveExtra: (index: number) => void;
  onClearVoice: () => void;
  onNameChange: (name: string) => void;
  onDescriptionChange: (description: string) => void;
}) {
  return (
    <div className="flex flex-col gap-5 sm:flex-row">
      <ImageColumn
        form={form}
        errors={errors}
        disabled={disabled}
        onPick={onPick}
        onSetMain={onSetMain}
        onRemoveExtra={onRemoveExtra}
      />
      <div className="flex min-w-0 flex-1 flex-col gap-4">
        <div>
          <label htmlFor="character-name" className="mb-1.5 block text-[13px] font-medium">
            名称 <span className="text-destructive">*</span>
          </label>
          <Input
            id="character-name"
            value={form.name}
            placeholder="例如：林晚"
            disabled={disabled}
            aria-invalid={!!errors.name}
            onChange={(event) => onNameChange(event.target.value)}
          />
          {errors.name && (
            <p role="alert" className="text-destructive mt-1.5 text-xs">
              {errors.name}
            </p>
          )}
        </div>

        <div>
          <div className="mb-1.5 text-[13px] font-medium">
            声音 <span className="text-muted-foreground text-xs font-normal">可选</span>
          </div>
          {form.voice ? (
            <VoiceCard
              voice={form.voice}
              disabled={disabled}
              onPick={() => onPick("voice")}
              onClear={onClearVoice}
            />
          ) : (
            <button
              type="button"
              data-slot="voice-empty"
              disabled={disabled}
              onClick={() => onPick("voice")}
              className="text-muted-foreground hover:bg-muted/60 focus-visible:ring-ring/50 flex h-12 w-full items-center justify-center gap-2 rounded-lg border border-dashed text-[13px] transition-colors outline-none focus-visible:ring-3 disabled:opacity-50"
            >
              <AudioLines className="size-4" />
              从资产库选择一段音频
            </button>
          )}
          <p className="text-muted-foreground mt-1.5 text-xs">
            作为「参考音频」交给支持的视频模型；模型不支持时只用形象。
          </p>
        </div>

        <div>
          <label htmlFor="character-desc" className="mb-1.5 block text-[13px] font-medium">
            角色设定 <span className="text-muted-foreground text-xs font-normal">可选</span>
          </label>
          <Textarea
            id="character-desc"
            value={form.description}
            placeholder="外貌、性格、口头禅……方便你和团队查看"
            disabled={disabled}
            aria-invalid={!!errors.description}
            onChange={(event) => onDescriptionChange(event.target.value)}
            className="min-h-24"
          />
          <div className="text-muted-foreground mt-1.5 flex text-xs">
            <span>只展示，不会自动加进提示词</span>
            <span
              className={cn(
                "ml-auto tabular-nums",
                [...form.description].length > DESC_MAX && "text-destructive",
              )}
            >
              {[...form.description].length}/{DESC_MAX}
            </span>
          </div>
          {errors.description && (
            <p role="alert" className="text-destructive mt-1 text-xs">
              {errors.description}
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
