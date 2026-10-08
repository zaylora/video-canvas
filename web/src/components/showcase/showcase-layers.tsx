import { useEffect, useRef, useState } from "react";

import type { ShowcaseItemDto } from "@/api/showcase/type";
import type { ShowcasePlayer } from "@/hooks/use-showcase-player";
import { DURATION, ms } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { placeholderBackground } from "@/utils/home/placeholder";
import { fallbackHue, nextIndex } from "@/utils/showcase/rules";

/** 一层在溶解里的角色：current 渐显盖上来，prev 垫在下面等被盖住，next 藏着预加载 */
type LayerRole = "current" | "prev" | "next";

/** 各角色的叠放顺序：新的盖在旧的上面，预加载的垫在最底下 */
const Z_INDEX: Record<LayerRole, number> = { current: 2, prev: 1, next: 0 };

/**
 * 背景里的一层作品：渐变底 → 封面 → 视频，三层叠着。
 * 封面先出现，视频首帧可播后淡入盖住封面（封面就是第一帧，视觉上无缝）；
 * 封面或视频加载失败时底下的渐变兜着，页面不会空。
 * 只有当前层会真的播放，下一层只预加载。
 */
function ShowcaseLayer({
  item,
  role,
  player,
}: {
  item: ShowcaseItemDto;
  role: LayerRole;
  player: ShowcasePlayer;
}) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const [videoOn, setVideoOn] = useState(false);
  const [videoFailed, setVideoFailed] = useState(false);
  const [posterFailed, setPosterFailed] = useState(false);
  const { playVideo, paused, hidden, settle, reducedMotion } = player;
  const isCurrent = role === "current";
  const shouldPlay = isCurrent && playVideo && !paused && !hidden && !videoFailed;

  /** 轮到当前：从后台设置的起始秒开始 */
  useEffect(() => {
    const video = videoRef.current;
    if (isCurrent && video && video.readyState > 0) video.currentTime = item.startSec;
  }, [isCurrent, item.startSec]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;
    if (shouldPlay) video.play().catch(() => undefined);
    else video.pause();
  }, [shouldPlay]);

  /** 视频坏了就停在封面，照常计时，不让进度卡死 */
  useEffect(() => {
    if (isCurrent && videoFailed) settle(item.id);
  }, [isCurrent, videoFailed, settle, item.id]);

  return (
    <div
      data-role={role}
      aria-hidden
      className="absolute inset-0 transition-opacity"
      style={{
        zIndex: Z_INDEX[role],
        opacity: role === "next" ? 0 : 1,
        background: placeholderBackground(fallbackHue(item.id)),
        transitionDuration: ms(reducedMotion ? DURATION.base : DURATION.dissolve),
      }}
    >
      {item.posterUrl && !posterFailed && (
        <img
          src={item.posterUrl}
          alt=""
          draggable={false}
          onError={() => setPosterFailed(true)}
          className="absolute inset-0 size-full object-cover"
        />
      )}
      {playVideo && !videoFailed && role !== "prev" && (
        <video
          ref={videoRef}
          src={item.videoUrl}
          muted
          loop
          playsInline
          preload="auto"
          onLoadedMetadata={(event) => {
            if (item.startSec > 0) event.currentTarget.currentTime = item.startSec;
          }}
          onPlaying={() => {
            setVideoOn(true);
            if (isCurrent) settle(item.id);
          }}
          onError={() => setVideoFailed(true)}
          className={cn(
            "absolute inset-0 size-full object-cover transition-opacity duration-180",
            videoOn ? "opacity-100" : "opacity-0",
          )}
        />
      )}
    </div>
  );
}

/**
 * 背景作品的全部图层：当前层、正在被溶解的上一层、预加载的下一层。
 * 图层按作品原有顺序渲染而不是按角色排，作品在角色之间切换时 DOM 位置不变，
 * 否则 React 移动 video 节点会让它被迫暂停、重新加载。
 */
export function ShowcaseLayers({ player }: { player: ShowcasePlayer }) {
  const { items, index, prevIndex, count } = player;
  const roles = new Map<string, LayerRole>();
  const assign = (i: number | null, role: LayerRole) => {
    const item = i === null ? undefined : items[i];
    if (item && !roles.has(item.id)) roles.set(item.id, role);
  };
  assign(index, "current");
  assign(prevIndex, "prev");
  assign(count > 1 ? nextIndex(index, count) : null, "next");

  return (
    <div data-slot="showcase-layers" className="absolute inset-0">
      {items.map((item) => {
        const role = roles.get(item.id);
        return role && <ShowcaseLayer key={item.id} item={item} role={role} player={player} />;
      })}
    </div>
  );
}
