import { useRef, useState } from "react";
import { AnimatePresence, motion } from "motion/react";
import { Loader2, Music, Play, Plus, RotateCw, X } from "lucide-react";

import type { Capabilities, RefKind } from "@/api/model/type";
import { useIsMobile } from "@/hooks/use-mobile";
import { cn } from "@/lib/utils";
import type { ComposerRef } from "@/utils/conversation/submission";
import {
  addSlot,
  collapsedWidth,
  expandedWidth,
  refSlot,
  type RefSize,
} from "@/utils/conversation/ref-layout";
import { acceptableRefKinds, refMax } from "@/utils/tasks/capabilities";

/** 各种参考素材在提示里的叫法 */
const KIND_NAME: Record<RefKind, string> = { image: "图片", video: "视频", audio: "音频" };

/** 桌面 / 手机上单张参考图的尺寸（px） */
const SIZE_DESKTOP: RefSize = { w: 56, h: 72 };
const SIZE_MOBILE: RefSize = { w: 44, h: 56 };

/** 摆放变化（叠放 ↔ 展开、加号块 ↔ 小圆钮）的过渡：阻尼偏大的弹簧，到位干脆，不来回晃 */
const LAYOUT_SPRING = { type: "spring", stiffness: 380, damping: 32, mass: 0.9 } as const;

/** 放大预览从下往上长出来的过渡 */
const PEEK_TRANSITION = { duration: 0.24, ease: [0.22, 1, 0.36, 1] } as const;

/** 上传块（没有参考图或展开时）悬停 / 聚焦：摆正并轻微放大 */
const TILE_ACTIVE = { rotate: 0, scale: 1.06 } as const;

/**
 * 输入卡片左侧的参考图区：
 * - 没有参考图：一个歪着的上传块，悬停摆正并轻微放大。
 * - 有参考图：上传块缩成压在右下角的小加号；多张参考图叠成一摞。
 * - 鼠标悬停（或键盘聚焦）：这一摞向右一字展开，小加号变回上传块排在最后；
 *   展开的内容浮在输入框文字上方，不会把文字挤开。
 * - 悬停某一张：它的大图从下往上放大弹出，并出现移除按钮。
 * 上传中显示转圈，失败显示重试；超出当前模型上限的标黄；模型不支持参考图时上传块禁用并写明原因。
 * 点上传块选文件；粘贴和拖入由输入卡片处理。
 * @param refs 参考图
 * @param max 当前模型最多几张，0 表示不支持
 * @param modelReady 有没有可用模型：没有时上传块禁用
 * @param onFiles 选了文件
 * @param onRetry 重试某张上传失败的
 * @param onRemove 移除某张
 */
export function ComposerRefs({
  refs,
  caps,
  modelReady,
  onFiles,
  onRetry,
  onRemove,
}: {
  refs: ComposerRef[];
  /** 当前模型能力：各种参考素材收不收、最多几个、单个多大 */
  caps: Capabilities | undefined;
  modelReady: boolean;
  onFiles: (files: File[]) => void;
  onRetry: (id: string) => void;
  onRemove: (id: string) => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);
  /** 最近一次指针类型：触屏没有悬停，点按后靠焦点展开；鼠标点击产生的焦点不该让它展开 */
  const lastPointer = useRef<string>("mouse");
  const isMobile = useIsMobile();
  const size = isMobile ? SIZE_MOBILE : SIZE_DESKTOP;
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const [peekId, setPeekId] = useState<string | null>(null);
  /** 鼠标在收起状态下停在右下角小加号上：这时点它直接上传，不展开这一摞 */
  const [onBadge, setOnBadge] = useState(false);

  const expanded = refs.length === 0 || ((hovered || focused) && !onBadge);
  const add = addSlot(refs.length, expanded, size);
  const peekIndex = peekId ? refs.findIndex((ref) => ref.id === peekId) : -1;
  const peek = peekIndex >= 0 && expanded ? refs[peekIndex] : null;

  /** 模型收的参考素材种类，以及每种的上限（与画布参考卡同一套规则） */
  const accepted = acceptableRefKinds(caps);
  const usedOf = (kind: RefKind) => refs.filter((ref) => ref.kind === kind).length;
  const allFull =
    accepted.length > 0 && accepted.every((kind) => usedOf(kind) >= refMax(caps, kind));
  const limitText = accepted
    .map((kind) => `${KIND_NAME[kind]}最多 ${refMax(caps, kind)} 个`)
    .join("、");
  const disabledReason = !modelReady
    ? "暂无可用模型"
    : accepted.length === 0
      ? "当前模型不支持参考素材"
      : allFull
        ? `参考素材已满（${limitText}）`
        : null;
  /** 每份参考在自己种类里排第几：超出该种类上限的标黄（上限 0 即模型不收，全部标黄） */
  const rankInKind = (index: number) =>
    refs.slice(0, index).filter((ref) => ref.kind === refs[index].kind).length;
  const addActive =
    disabledReason === null ? (add.tile ? TILE_ACTIVE : { scale: 1.15 }) : undefined;

  return (
    <motion.div
      data-slot="composer-refs"
      className={cn("relative shrink-0", refs.length > 0 && "z-10")}
      style={{ height: size.h }}
      initial={false}
      animate={{ width: collapsedWidth(refs.length, size) }}
      transition={LAYOUT_SPRING}
      onPointerDownCapture={(event) => {
        lastPointer.current = event.pointerType;
      }}
      onPointerEnter={(event) => event.pointerType === "mouse" && setHovered(true)}
      onPointerLeave={(event) => {
        if (event.pointerType !== "mouse") return;
        setHovered(false);
        setPeekId(null);
      }}
      onFocus={(event) => {
        // 键盘聚焦（:focus-visible）和触屏点按才展开；鼠标点上传按钮后焦点留在里面，不算
        if (lastPointer.current === "touch" || event.target.matches(":focus-visible"))
          setFocused(true);
      }}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget)) setFocused(false);
      }}
    >
      {/* 展开时铺在整行下面的透明区域：盖住各张之间的间隙，鼠标走向最右边的上传块时不会被当成移出 */}
      <div
        aria-hidden
        className="absolute top-0 left-0"
        style={{
          width: expandedWidth(refs.length, size),
          height: size.h,
          pointerEvents: expanded && refs.length > 0 ? "auto" : "none",
        }}
      />

      <AnimatePresence initial={false}>
        {refs.map((ref, index) => {
          const slot = refSlot(index, expanded, size);
          const overLimit = rankInKind(index) >= refMax(caps, ref.kind);
          const showRemove = expanded && (isMobile || peekId === ref.id || focused);
          return (
            <motion.div
              key={ref.id}
              tabIndex={0}
              title={
                overLimit
                  ? refMax(caps, ref.kind) > 0
                    ? `超出当前模型的${KIND_NAME[ref.kind]}参考数量（最多 ${refMax(caps, ref.kind)} 个）`
                    : `当前模型不支持${KIND_NAME[ref.kind]}参考`
                  : ref.name
              }
              className={cn(
                "bg-muted focus-visible:ring-ring absolute top-0 left-0 rounded-[10px] shadow-sm ring-1 ring-black/10 outline-none focus-visible:ring-2",
                overLimit && "ring-2 ring-amber-500",
              )}
              style={{ width: size.w, height: size.h, zIndex: slot.z }}
              initial={{ opacity: 0, scale: 0.8, x: slot.x, y: slot.y, rotate: slot.rotate }}
              animate={{
                opacity: slot.opacity,
                scale: 1,
                x: slot.x,
                y: slot.y,
                rotate: slot.rotate,
              }}
              exit={{ opacity: 0, scale: 0.8 }}
              transition={LAYOUT_SPRING}
              onPointerEnter={(event) => event.pointerType === "mouse" && setPeekId(ref.id)}
              onPointerLeave={(event) => event.pointerType === "mouse" && setPeekId(null)}
            >
              <RefMedia item={ref} />
              {ref.status === "uploading" && (
                <span className="absolute inset-0 grid place-items-center rounded-[10px] bg-black/40 text-white">
                  <Loader2 className="size-4 animate-spin" />
                </span>
              )}
              {ref.status === "error" && (
                <button
                  type="button"
                  onClick={() => onRetry(ref.id)}
                  className="absolute inset-0 grid place-items-center rounded-[10px] bg-black/55 text-[11px] text-white"
                >
                  <span className="grid justify-items-center gap-0.5">
                    <RotateCw className="size-3.5" />
                    重试
                  </span>
                </button>
              )}
              {showRemove && (
                <button
                  type="button"
                  aria-label={`移除 ${ref.name}`}
                  onClick={() => onRemove(ref.id)}
                  className="bg-foreground text-background absolute -top-1.5 -left-1.5 grid size-5 place-items-center rounded-full"
                >
                  <X className="size-3" />
                </button>
              )}
            </motion.div>
          );
        })}
      </AnimatePresence>

      <motion.button
        type="button"
        disabled={disabledReason !== null}
        aria-label="上传参考图"
        title={disabledReason ?? `上传参考素材（${limitText}），也可以直接粘贴或拖入`}
        onClick={() => inputRef.current?.click()}
        className={cn(
          "focus-visible:ring-ring/50 absolute top-0 left-0 grid place-items-center border-dashed outline-none transition-colors duration-200 focus-visible:ring-3 disabled:cursor-not-allowed disabled:opacity-60",
          add.tile
            ? "bg-muted text-muted-foreground border-foreground/20 hover:text-foreground"
            : "bg-foreground/80 text-background ring-card border-transparent ring-2",
        )}
        style={{ zIndex: 20 }}
        initial={false}
        animate={{
          x: add.x,
          y: add.y,
          width: add.w,
          height: add.h,
          borderRadius: add.radius,
          rotate: add.rotate,
          borderWidth: add.tile ? 1 : 0,
        }}
        onPointerEnter={(event) => {
          // 收起时指到小加号上不展开；已经展开（从缩略图移过来）时它是整块上传块，保持展开
          if (event.pointerType === "mouse" && !expanded) setOnBadge(true);
        }}
        onPointerLeave={() => setOnBadge(false)}
        whileHover={addActive}
        whileFocus={addActive}
        whileTap={disabledReason === null ? { scale: 0.95 } : undefined}
        transition={LAYOUT_SPRING}
      >
        <Plus className={"size-4.5"} />
      </motion.button>

      <AnimatePresence>
        {peek?.url && peek.kind !== "audio" && (
          <motion.div
            key={peek.id}
            className="pointer-events-none absolute z-30"
            style={{
              left: refSlot(peekIndex, true, size).x,
              bottom: size.h + 14,
              transformOrigin: "50% 100%",
            }}
            initial={{ opacity: 0, scaleY: 0.4, scaleX: 0.85, y: 24 }}
            animate={{ opacity: 1, scaleY: 1, scaleX: 1, y: 0 }}
            exit={{ opacity: 0, scaleY: 0.6, scaleX: 0.9, y: 14 }}
            transition={PEEK_TRANSITION}
          >
            {peek.kind === "video" ? (
              <video
                src={peek.url}
                autoPlay
                muted
                loop
                playsInline
                className="bg-card border-border max-h-72 max-w-64 rounded-xl border object-contain shadow-2xl"
              />
            ) : (
              <img
                src={peek.url}
                alt={`${peek.name} 的大图`}
                className="bg-card border-border max-h-72 max-w-64 rounded-xl border object-contain shadow-2xl"
              />
            )}
          </motion.div>
        )}
      </AnimatePresence>

      <input
        ref={inputRef}
        type="file"
        accept={accepted.map((kind) => `${kind}/*`).join(",")}
        multiple
        className="sr-only"
        tabIndex={-1}
        aria-hidden
        onChange={(event) => {
          const files = Array.from(event.target.files ?? []);
          event.target.value = "";
          setOnBadge(false);
          // 选完文件立刻收起：此时鼠标可能还停在这块区域上，不会再触发「移出」
          setHovered(false);
          setFocused(false);
          setPeekId(null);
          if (files.length > 0) onFiles(files);
        }}
      />
    </motion.div>
  );
}

/** 一份参考素材的缩略内容：图片画图，视频取首帧并叠播放标，音频是音符加文件名 */
function RefMedia({ item }: { item: ComposerRef }) {
  if (item.kind === "audio") {
    return (
      <span className="text-muted-foreground grid size-full place-items-center gap-0.5 rounded-[10px] px-1 text-center">
        <Music className="size-4" />
        <span className="line-clamp-2 text-[10px] leading-tight break-all">{item.name}</span>
      </span>
    );
  }
  if (!item.url) return null;
  if (item.kind === "video") {
    return (
      <>
        <video
          src={item.url}
          muted
          preload="metadata"
          playsInline
          className="size-full rounded-[10px] object-cover"
        />
        <span className="absolute inset-0 m-auto grid size-6 place-items-center rounded-full bg-black/50 text-white">
          <Play className="size-3 fill-current" />
        </span>
      </>
    );
  }
  return (
    <img
      src={item.url}
      alt={item.name}
      draggable={false}
      className="size-full rounded-[10px] object-cover"
    />
  );
}
