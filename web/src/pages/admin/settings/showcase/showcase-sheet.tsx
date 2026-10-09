import { useEffect, useMemo, useRef, useState, type DragEvent } from "react";
import { ImagePlus, Loader2, Upload } from "lucide-react";
import { toast } from "sonner";

import { createShowcaseItem, updateShowcaseItem } from "@/api/admin/showcase";
import type { ShowcaseAdminItem } from "@/api/admin/showcase/type";
import { uploadAsset } from "@/api/asset";
import type { AssetDto } from "@/api/asset/type";
import { FormField } from "@/components/admin-ui/form-field";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { captureVideoFrame } from "@/utils/showcase/poster";
import { PROMPT_MAX } from "@/utils/showcase/rules";

import { useAliveRef } from "../../use-admin";

/** 封面的来源：保持现有的、自动截取第一帧、自己上传一张图、不要封面 */
type CoverMode = "keep" | "auto" | "upload" | "none";

/** 按字符数（不是 UTF-16 码元）算字数，和后端「≤80 字」一致 */
const charCount = (text: string) => [...text].length;

/**
 * 上传视频 / 编辑作品的抽屉（宽 460px，设计稿）。
 * 上传：拖入或选择视频，选完立刻上传，上传完才能保存；封面默认由浏览器从起始秒截一帧一起传，
 * 不需要后端装 ffmpeg；也可以自己上传一张图。
 * 编辑：能改提示词、模型标签、起始秒、封面和启用状态；「替换视频」重新选一个文件，保存时换掉。
 * @param item 正在编辑的作品；null 表示上传新视频
 * @param open 是否打开
 * @param onSaved 保存成功，带回服务端返回的作品
 */
export function ShowcaseSheet({
  open,
  item,
  onClose,
  onSaved,
}: {
  open: boolean;
  item: ShowcaseAdminItem | null;
  onClose: () => void;
  onSaved: (item: ShowcaseAdminItem) => void;
}) {
  return (
    <Sheet open={open} onOpenChange={(next) => !next && onClose()}>
      <SheetContent className="data-[side=right]:sm:max-w-[460px]">
        {/* 换一条作品或重新打开就换 key，表单状态整份重置 */}
        <SheetForm key={item?.id ?? "new"} item={item} onClose={onClose} onSaved={onSaved} />
      </SheetContent>
    </Sheet>
  );
}

function SheetForm({
  item,
  onClose,
  onSaved,
}: {
  item: ShowcaseAdminItem | null;
  onClose: () => void;
  onSaved: (item: ShowcaseAdminItem) => void;
}) {
  const aliveRef = useAliveRef();
  const fileInput = useRef<HTMLInputElement>(null);
  const coverInput = useRef<HTMLInputElement>(null);
  const editing = item !== null;

  /** 本次新选的视频：新增时就是它，编辑时表示「替换视频」 */
  const [videoFile, setVideoFile] = useState<File | null>(null);
  const [videoAsset, setVideoAsset] = useState<AssetDto | null>(null);
  const [uploading, setUploading] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [coverMode, setCoverMode] = useState<CoverMode>(
    editing ? (item.poster_url ? "keep" : "none") : "auto",
  );
  const [coverFile, setCoverFile] = useState<File | null>(null);
  const [prompt, setPrompt] = useState(item?.prompt ?? "");
  const [modelLabel, setModelLabel] = useState(item?.model_label ?? "");
  const [startSec, setStartSec] = useState(String(item?.start_sec ?? 0));
  const [enabled, setEnabled] = useState(item?.enabled ?? true);
  const [saving, setSaving] = useState(false);
  const [touched, setTouched] = useState(false);

  const promptLength = charCount(prompt.trim());
  const start = Number(startSec);
  const errors = {
    prompt:
      promptLength === 0
        ? "提示词不能为空，它会显示在画面上"
        : promptLength > PROMPT_MAX
          ? `提示词不能超过 ${PROMPT_MAX} 字`
          : "",
    start:
      startSec.trim() === "" || !Number.isFinite(start) || start < 0
        ? "起始秒必须是不小于 0 的数字"
        : "",
    video: !editing && !videoAsset ? "请先上传视频" : "",
    cover: coverMode === "upload" && !coverFile ? "请选择一张封面图片" : "",
  };
  const invalid = Object.values(errors).some(Boolean);

  /** 选视频：立刻上传，上传期间不能保存。编辑时选新视频等于替换，旧封面不再对应，默认改成自动截取 */
  const pickVideo = async (file: File) => {
    setVideoFile(file);
    setVideoAsset(null);
    setUploading(true);
    if (coverMode === "keep") setCoverMode("auto");
    try {
      const asset = await uploadAsset(file);
      if (aliveRef.current) setVideoAsset(asset);
    } catch {
      // 全局 toast 已弹（类型不对、超限等），清掉选择让用户重选
      if (aliveRef.current) setVideoFile(null);
    } finally {
      if (aliveRef.current) setUploading(false);
    }
  };

  const handleDrop = (event: DragEvent) => {
    event.preventDefault();
    setDragging(false);
    const file = event.dataTransfer.files?.[0];
    if (!file) return;
    if (!file.type.startsWith("video/")) {
      toast.error("请拖入视频文件（MP4 / WebM）");
      return;
    }
    void pickVideo(file);
  };

  /** 算出要用的封面素材 ID：undefined 表示不动，null 表示清空 */
  const resolvePoster = async (): Promise<number | null | undefined> => {
    if (coverMode === "keep") return undefined;
    if (coverMode === "none") return editing ? null : undefined;
    let file: File | null = coverFile;
    if (coverMode === "auto") {
      if (!videoFile) return undefined;
      try {
        file = await captureVideoFrame(videoFile, start);
      } catch {
        toast.warning("没能自动截取封面，先不带封面保存，之后可以在编辑里上传一张");
        return editing ? null : undefined;
      }
    }
    if (!file) return undefined;
    return Number((await uploadAsset(file)).id);
  };

  const save = async () => {
    setTouched(true);
    if (invalid || saving || uploading) return;
    setSaving(true);
    try {
      const poster = await resolvePoster();
      const saved = editing
        ? await updateShowcaseItem(item.id, {
            prompt: prompt.trim(),
            model_label: modelLabel.trim(),
            start_sec: start,
            enabled,
            ...(videoAsset && { asset_id: Number(videoAsset.id) }),
            ...(poster !== undefined && { poster_asset_id: poster }),
          })
        : await createShowcaseItem({
            asset_id: Number(videoAsset!.id),
            poster_asset_id: poster ?? null,
            prompt: prompt.trim(),
            model_label: modelLabel.trim(),
            start_sec: start,
            enabled,
          });
      if (!aliveRef.current) return;
      toast.success("已保存，前台立即生效");
      onSaved(saved);
      onClose();
    } catch {
      // 全局 toast 已弹，表单保留当前输入
    } finally {
      if (aliveRef.current) setSaving(false);
    }
  };

  /** 本地选的视频预览：对象地址只在文件变化时生成，卸载或换文件时释放 */
  const localUrl = useMemo(() => (videoFile ? URL.createObjectURL(videoFile) : null), [videoFile]);
  useEffect(() => () => (localUrl ? URL.revokeObjectURL(localUrl) : undefined), [localUrl]);
  const videoPreview = localUrl ?? (editing ? item.video_url : null);

  /** 封面可选的来源：编辑且没换视频时能保持现有；有本地视频时能自动截取；都能上传或不要 */
  const coverOptions: { value: CoverMode; label: string }[] = [
    ...(editing && item.poster_url && !videoFile
      ? [{ value: "keep" as const, label: "保持现有" }]
      : []),
    ...(videoFile ? [{ value: "auto" as const, label: "自动截取第一帧" }] : []),
    { value: "upload", label: "上传图片" },
    { value: "none", label: "不要封面" },
  ];

  return (
    <form
      className="flex min-h-0 flex-1 flex-col"
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <SheetHeader className="border-b px-5 py-4.5">
        <SheetTitle className="text-base font-semibold">
          {editing ? "编辑作品" : "上传视频"}
        </SheetTitle>
        <SheetDescription className="sr-only">
          显示在登录页画面左下角的是「生成它的那句话」，访客可以一键做同款。
        </SheetDescription>
      </SheetHeader>

      <div className="grid min-h-0 flex-1 content-start gap-4.5 overflow-y-auto p-5">
        <FormField label="视频" error={touched ? errors.video : undefined}>
          {videoPreview ? (
            <div className="bg-stage relative aspect-video overflow-hidden rounded-xl">
              <video
                key={videoPreview}
                src={videoPreview}
                muted
                loop
                playsInline
                autoPlay
                className="absolute inset-0 size-full object-cover"
              />
              {uploading && (
                <div className="bg-background/70 absolute inset-0 grid place-items-center text-sm backdrop-blur-sm">
                  <span className="inline-flex items-center gap-2">
                    <Loader2 className="size-4 animate-spin" />
                    上传中…
                  </span>
                </div>
              )}
              {!uploading && (
                <button
                  type="button"
                  onClick={() => fileInput.current?.click()}
                  className="bg-cover-scrim text-cover-foreground focus-visible:ring-cover-foreground/70 absolute right-2 bottom-2 inline-flex h-7 items-center gap-1.5 rounded-full px-2.5 text-xs outline-none focus-visible:ring-2"
                >
                  <Upload className="size-3.5" />
                  替换视频
                </button>
              )}
            </div>
          ) : (
            <button
              type="button"
              onClick={() => fileInput.current?.click()}
              onDragOver={(event) => {
                event.preventDefault();
                setDragging(true);
              }}
              onDragLeave={() => setDragging(false)}
              onDrop={handleDrop}
              className={cn(
                "text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring/50 grid aspect-video w-full place-items-center rounded-xl border-[1.5px] border-dashed p-4 text-center outline-none focus-visible:ring-3",
                dragging && "bg-muted text-foreground border-foreground",
              )}
            >
              <div>
                <Upload className="mx-auto size-5" />
                <b className="text-foreground mt-2 mb-1 block font-medium">
                  拖入视频，或点击选择文件
                </b>
                <span className="text-xs">
                  MP4 / WebM · 建议横屏 16:9、720p、5–15 秒、10 MB 以内
                </span>
              </div>
            </button>
          )}
          <input
            ref={fileInput}
            type="file"
            accept="video/*"
            hidden
            onChange={(event) => {
              const file = event.target.files?.[0];
              event.target.value = "";
              if (file) void pickVideo(file);
            }}
          />
        </FormField>

        <FormField
          label={
            <>
              生成它的那句话
              <span className="text-muted-foreground font-normal">
                显示在画面左下角，「做同款」会把它带进首页输入框
              </span>
            </>
          }
          htmlFor="showcase-prompt"
          error={touched ? errors.prompt : undefined}
        >
          <Textarea
            id="showcase-prompt"
            rows={3}
            value={prompt}
            aria-invalid={touched && !!errors.prompt}
            onChange={(event) => setPrompt(event.target.value)}
          />
          <span
            className={cn(
              "text-right text-xs tabular-nums",
              promptLength > PROMPT_MAX ? "text-destructive" : "text-muted-foreground",
            )}
          >
            {promptLength} / {PROMPT_MAX}
          </span>
        </FormField>

        <div className="grid grid-cols-2 gap-3 max-md:grid-cols-1">
          <FormField label="模型标签（可选）" htmlFor="showcase-model">
            <Input
              id="showcase-model"
              maxLength={40}
              placeholder="如 Seedance 2.0"
              value={modelLabel}
              onChange={(event) => setModelLabel(event.target.value)}
            />
          </FormField>
          <FormField
            label="从第几秒开始"
            htmlFor="showcase-start"
            error={touched ? errors.start : undefined}
          >
            <Input
              id="showcase-start"
              inputMode="decimal"
              className="tabular-nums"
              value={startSec}
              aria-invalid={touched && !!errors.start}
              onChange={(event) => setStartSec(event.target.value)}
            />
          </FormField>
        </div>

        <FormField
          label="封面"
          error={touched ? errors.cover : undefined}
          hint="封面会先于视频显示，也是视频加载失败、省流量、减少动态效果时的画面。"
        >
          <div className="flex gap-2">
            {coverOptions.map((option) => (
              <label
                key={option.value}
                className="has-checked:border-foreground flex h-9 flex-1 cursor-pointer items-center gap-2 rounded-lg border px-3 text-sm"
              >
                <input
                  type="radio"
                  name="cover"
                  checked={coverMode === option.value}
                  onChange={() => setCoverMode(option.value)}
                />
                {option.label}
              </label>
            ))}
          </div>
          {coverMode === "keep" && item?.poster_url && (
            <img
              src={item.poster_url}
              alt="当前封面"
              className="bg-muted aspect-video w-40 rounded-lg object-cover"
            />
          )}
          {coverMode === "upload" && (
            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => coverInput.current?.click()}
              >
                <ImagePlus />
                {coverFile ? "重新选择" : "选择图片"}
              </Button>
              {coverFile && (
                <span className="text-muted-foreground min-w-0 truncate text-xs">
                  {coverFile.name}
                </span>
              )}
              <input
                ref={coverInput}
                type="file"
                accept="image/*"
                hidden
                onChange={(event) => {
                  const file = event.target.files?.[0];
                  event.target.value = "";
                  if (file) setCoverFile(file);
                }}
              />
            </div>
          )}
        </FormField>

        <div className="flex items-center justify-between gap-4">
          <div>
            <label htmlFor="showcase-enabled" className="text-sm font-medium">
              启用
            </label>
            <p className="text-muted-foreground text-xs">关闭后保留在列表，不参与轮播</p>
          </div>
          <Switch id="showcase-enabled" checked={enabled} onCheckedChange={setEnabled} />
        </div>
      </div>

      <SheetFooter className="flex-row justify-end border-t px-5 py-3.5">
        <Button type="button" variant="ghost" onClick={onClose} disabled={saving}>
          取消
        </Button>
        <Button type="submit" disabled={saving || uploading}>
          {saving && <Loader2 className="animate-spin" />}
          保存
        </Button>
      </SheetFooter>
    </form>
  );
}
