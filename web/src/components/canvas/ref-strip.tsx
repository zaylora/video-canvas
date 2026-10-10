import { useEffect, useRef, useState, type ReactNode } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { Link2, Loader2, Play, Plus, Upload, X } from "lucide-react";

import { uploadAsset } from "@/api/asset";
import type { RefKind } from "@/api/model/type";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { DURATION, EASE_OUT, SPRING, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { ParamAsset } from "@/types";
import type { RefKey } from "@/utils/tasks/capabilities";

import { RefThumb, usePromptRefs, type RefSource } from "./prompt-mention";

/** 引用条上的一个上游：接进来的节点，以及当前生成方式用不用得上它 */
export type RefItem = RefSource & {
  /** 当前生成方式用得上；用不上的（比如文生视频接了图片）变淡 */
  used: boolean;
  /** 上游还在生成 */
  running?: boolean;
};

/** 手动上传的参考素材：不是画布节点，只能在这里移除 */
export type ManualRef = {
  key: RefKey;
  assetId: string;
  asset?: ParamAsset;
};

/** 当前生成方式能上传哪种参考素材 */
export type UploadKind = {
  key: RefKey;
  kind: RefKind;
  label: string;
  /** 单个文件上限，MB */
  maxMb: number;
};

const ACCEPT: Record<RefKind, string> = { image: "image/*", video: "video/*", audio: "audio/*" };

const TILE =
  "relative grid size-12 shrink-0 place-items-center overflow-hidden rounded-md bg-foreground/8 outline-none ring-1 ring-chrome-border transition-[box-shadow,opacity] duration-120 focus-visible:ring-2 focus-visible:ring-node-ring";

/** 缩略图右上角的 ×：连线来的是断开连线，手动上传的是移除素材 */
function RemoveButton({
  label,
  disabled,
  onClick,
}: {
  label: string;
  disabled?: boolean;
  onClick: () => void;
}) {
  return (
    <motion.button
      type="button"
      whileTap={TAP}
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
      className="focus-visible:ring-node-ring absolute top-[3px] right-[3px] z-10 grid size-4 place-items-center rounded-[5px] bg-black/60 text-white/80 outline-none transition-colors hover:bg-black/85 hover:text-white focus-visible:ring-2 disabled:opacity-40 [&_svg]:size-2.5 [&_svg]:stroke-[2.4]"
    >
      <X />
    </motion.button>
  );
}

/** 缩略图角上的小标：左上是「来自连线」，右下标视频 */
function Badge({ className, children }: { className: string; children: ReactNode }) {
  return (
    <span
      className={cn(
        "absolute grid size-4 place-items-center rounded-[5px] bg-black/60 text-white [&_svg]:size-2.5 [&_svg]:stroke-[2.4]",
        className,
      )}
    >
      {children}
    </span>
  );
}

/**
 * 引用条（设计稿 6.7）：所有种类节点的面板顶部都有，列出接进来的全部上游，按连线先后排。
 * 点一格就在提示词光标处插入它的 chip；悬停时 chip 和画布上的源节点一起描边。
 * 每格右上角的 × 删掉这份引用：连线来的断开连线（⌘Z 可恢复），手动上传的移除素材。
 * 末尾的「+」从画布里挑接得上的素材（自动连线），或者上传一份参考素材。
 */
export function RefStrip({
  items,
  manual,
  candidates,
  onLink,
  onUnlink,
  uploadKinds,
  onUpload,
  onRemoveManual,
  errors,
  disabled,
}: {
  items: RefItem[];
  manual: ManualRef[];
  /** 打开「+」菜单时现取：画布里还没连、接得上的素材 */
  candidates: () => RefSource[];
  onLink: (source: RefSource) => void;
  /** 断开某个上游的连线 */
  onUnlink: (sourceId: string) => void;
  uploadKinds: UploadKind[];
  onUpload: (key: RefKey, assetId: string | number, asset: ParamAsset) => void;
  onRemoveManual: (key: RefKey, assetId: string) => void;
  /** 参考素材相关的校验错误 */
  errors: string[];
  disabled?: boolean;
}) {
  const refs = usePromptRefs();
  const reduce = useReducedMotion();
  const scroller = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const [choices, setChoices] = useState<RefSource[]>([]);
  const [uploading, setUploading] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);

  // 新连上的素材排在最后，滚到能看见它
  const count = items.length + manual.length;
  const lastCount = useRef(count);
  useEffect(() => {
    if (count > lastCount.current)
      scroller.current?.scrollTo({ left: scroller.current.scrollWidth, behavior: "smooth" });
    lastCount.current = count;
  }, [count]);

  const upload = async (file: File) => {
    const target = uploadKinds.find((item) => file.type.startsWith(`${item.kind}/`));
    if (!target) {
      setUploadError("当前生成方式用不了这种文件");
      return;
    }
    if (file.size > target.maxMb * 1024 * 1024) {
      setUploadError(`单个${target.label}不能超过 ${target.maxMb} MB`);
      return;
    }
    setUploading(true);
    setUploadError(null);
    try {
      const uploaded = await uploadAsset(file);
      onUpload(target.key, uploaded.id, {
        url: uploaded.url,
        fileName: uploaded.fileName ?? file.name,
        mediaType: target.kind,
      });
    } catch {
      setUploadError("上传失败，请重试");
    } finally {
      setUploading(false);
    }
  };

  const enter = reduce
    ? { initial: { opacity: 0 }, animate: { opacity: 1 } }
    : { initial: { opacity: 0, scale: 0.6 }, animate: { opacity: 1, scale: 1 } };
  const exit = { opacity: 0, transition: { duration: DURATION.base * 0.7, ease: EASE_OUT } };
  const messages = [uploadError, ...errors].filter(Boolean);

  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <div
        ref={scroller}
        className="nowheel -m-0.5 flex min-w-0 items-center gap-2 overflow-x-auto p-0.5 [scrollbar-width:none]"
      >
        <AnimatePresence initial={false}>
          {items.map((item) => {
            const lit = refs?.highlight === item.id;
            const empty = item.kind === "script" ? !item.text?.trim() : !item.src;
            const note =
              item.running || empty ? " · 还没生成" : item.used ? "" : " · 当前生成方式用不到";
            return (
              <motion.span
                key={item.id}
                layout="position"
                {...enter}
                exit={exit}
                transition={SPRING}
                onMouseEnter={() => refs?.setHighlight(item.id)}
                onMouseLeave={() => refs?.setHighlight(null)}
                className={cn(
                  TILE,
                  "hover:ring-node-ring focus-within:ring-node-ring hover:ring-2",
                  lit && "ring-node-ring ring-2",
                  empty && "ring-foreground/12 bg-transparent ring-[1.5px] ring-inset",
                )}
              >
                <motion.button
                  type="button"
                  whileTap={TAP}
                  title={`${item.label}${note}\n点击插入到提示词`}
                  aria-label={`引用：${item.label}，点击插入到提示词`}
                  onClick={() => refs?.insertRef(item)}
                  className={cn("size-full outline-none", !item.used && "opacity-40")}
                >
                  <RefThumb source={item} />
                </motion.button>
                <Badge className="pointer-events-none top-[3px] left-[3px]">
                  <Link2 />
                </Badge>
                {item.kind === "video" && !empty && (
                  <Badge className="pointer-events-none right-[3px] bottom-[3px]">
                    <Play />
                  </Badge>
                )}
                <RemoveButton
                  label={`断开「${item.label}」的引用`}
                  disabled={disabled}
                  onClick={() => {
                    refs?.setHighlight(null);
                    onUnlink(item.id);
                  }}
                />
              </motion.span>
            );
          })}
          {manual.map((ref) => (
            <motion.span
              key={`${ref.key}:${ref.assetId}`}
              layout="position"
              {...enter}
              exit={exit}
              transition={SPRING}
              title={`${ref.asset?.fileName ?? `素材 #${ref.assetId}`}（手动添加）`}
              className={TILE}
            >
              <RefThumb
                source={{
                  id: ref.assetId,
                  kind: ref.asset?.mediaType ?? "image",
                  label: ref.asset?.fileName ?? "",
                  src: ref.asset?.url,
                  mediaType: ref.asset?.mediaType,
                }}
              />
              <RemoveButton
                label="移除这份素材"
                disabled={disabled}
                onClick={() => onRemoveManual(ref.key, ref.assetId)}
              />
            </motion.span>
          ))}
        </AnimatePresence>

        <DropdownMenu
          modal={false}
          onOpenChange={(open) => {
            if (open) setChoices(candidates());
          }}
        >
          <DropdownMenuTrigger
            disabled={disabled || uploading}
            aria-label="引用画布里的素材"
            title="引用画布里的素材（会自动连线）"
            className={cn(
              TILE,
              "text-muted-foreground hover:text-foreground bg-foreground/5 ring-foreground/8 hover:bg-foreground/10 disabled:opacity-50",
            )}
          >
            {uploading ? <Loader2 className="size-5 animate-spin" /> : <Plus className="size-5" />}
          </DropdownMenuTrigger>
          <DropdownMenuContent className="w-64" align="start" sideOffset={6}>
            <DropdownMenuGroup>
              <DropdownMenuLabel>画布中可引用 · 选中后自动连线</DropdownMenuLabel>
              {choices.length === 0 && (
                <p className="text-muted-foreground px-1.5 py-2 text-xs">
                  画布里没有能接进来的素材
                </p>
              )}
              {choices.map((choice) => (
                <DropdownMenuItem key={choice.id} onClick={() => onLink(choice)}>
                  <span className="bg-foreground/10 size-7 shrink-0 overflow-hidden rounded-md">
                    <RefThumb compact source={choice} />
                  </span>
                  <span className="min-w-0 flex-1 truncate">{choice.label}</span>
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
            {uploadKinds.length > 0 && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => inputRef.current?.click()}>
                  <Upload />
                  上传参考素材
                  <span className="text-muted-foreground ml-auto text-[11px]">
                    {uploadKinds.map((item) => item.label.replace("参考", "")).join(" / ")}
                  </span>
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      <input
        ref={inputRef}
        type="file"
        accept={uploadKinds.map((item) => ACCEPT[item.kind]).join(",")}
        className="sr-only"
        aria-hidden
        tabIndex={-1}
        onChange={(event) => {
          const file = event.target.files?.[0];
          event.target.value = "";
          if (file) void upload(file);
        }}
      />
      {messages.length > 0 && <p className="text-destructive text-xs">{messages[0]}</p>}
    </div>
  );
}
