import { useSyncExternalStore } from "react";
import { Pause, Play } from "lucide-react";

import type { AssetDto } from "@/api/asset/type";
import { cn } from "@/lib/utils";
import { formatMediaDuration } from "@/utils/canvas/media-lod";

/**
 * 全站同一时刻只放一条试听：资产页网格、编辑弹框、画布节点共用一个 audio 元素，
 * 点另一条会自动停掉上一条，不会几条声音叠在一起。
 */
let audio: HTMLAudioElement | null = null;
let playingId: string | null = null;
const listeners = new Set<() => void>();

const emit = () => listeners.forEach((listener) => listener());

/** 停止试听并清掉状态 */
function stop() {
  audio?.pause();
  playingId = null;
  emit();
}

/**
 * 点一条声音：正在放就停，否则切到它从头放；放完或出错自动复位。
 * @param asset 声音素材
 */
export function toggleVoice(asset: AssetDto) {
  if (playingId === asset.id) {
    stop();
    return;
  }
  audio ??= new Audio();
  audio.onended = stop;
  audio.onerror = stop;
  audio.src = asset.url;
  playingId = asset.id;
  emit();
  void audio.play().catch(stop);
}

/**
 * 这条声音是否正在试听。
 * @param assetId 声音素材 ID
 */
export function useVoicePlaying(assetId: string | undefined): boolean {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    () => assetId !== undefined && playingId === assetId,
    () => false,
  );
}

/** 一串固定的伪随机条高：只是装饰，同一条声音每次画出来一样 */
const waveHeights = (seed: string, bars: number): number[] => {
  let h = 2166136261;
  for (const ch of seed) h = Math.imul(h ^ ch.charCodeAt(0), 16777619);
  return Array.from({ length: bars }, () => {
    h = Math.imul(h ^ (h >>> 13), 1274126177);
    return 25 + ((h >>> 0) % 75);
  });
};

/**
 * 装饰性声波条（不是真实波形，真实波形要解码整段音频，列表里太重）。
 * @param seed 种子，通常用素材 ID，保证同一条声音长相稳定
 * @param bars 条数
 * @param active 是否正在播放：着色
 */
export function VoiceWave({
  seed,
  bars = 16,
  active,
  className,
}: {
  seed: string;
  bars?: number;
  active?: boolean;
  className?: string;
}) {
  return (
    <span aria-hidden className={cn("flex h-4 min-w-0 flex-1 items-center gap-px", className)}>
      {waveHeights(seed, bars).map((height, index) => (
        <span
          key={index}
          style={{ height: `${height}%` }}
          className={cn(
            "w-0.5 shrink-0 rounded-full bg-current transition-opacity duration-150 motion-reduce:transition-none",
            !active && "opacity-50",
          )}
        />
      ))}
    </span>
  );
}

/**
 * 圆形试听按钮：播放 / 暂停图标随状态切换。点击不冒泡，放在可点击的卡片上不会触发卡片本身。
 * @param asset 声音素材
 * @param className 外框样式（尺寸、底色由调用方定）
 */
export function VoicePlayButton({ asset, className }: { asset: AssetDto; className?: string }) {
  const playing = useVoicePlaying(asset.id);
  return (
    <button
      type="button"
      aria-label={playing ? "暂停试听" : "试听"}
      aria-pressed={playing}
      onClick={(event) => {
        event.stopPropagation();
        toggleVoice(asset);
      }}
      onPointerDown={(event) => event.stopPropagation()}
      className={cn(
        "focus-visible:ring-ring/60 grid size-6 shrink-0 place-items-center rounded-full outline-none focus-visible:ring-2 [&_svg]:size-3",
        className,
      )}
    >
      {playing ? <Pause /> : <Play />}
    </button>
  );
}

/**
 * 一行声音条：试听键 + 声波 + 时长。资产页卡片、画布节点的底部都用它。
 * @param asset 声音素材
 * @param dimmed 声音未生效（目标不收参考音频）时变淡
 * @param onCover 压在封面遮罩上：按钮和文字用封面前景色，不跟随主题前景
 */
export function VoiceBar({
  asset,
  dimmed,
  onCover,
  className,
}: {
  asset: AssetDto;
  dimmed?: boolean;
  onCover?: boolean;
  className?: string;
}) {
  const playing = useVoicePlaying(asset.id);
  const duration = formatMediaDuration(asset.durationMs);
  return (
    <span
      data-slot="voice-bar"
      className={cn(
        "flex min-w-0 items-center gap-2 text-xs tabular-nums",
        dimmed && "opacity-50",
        className,
      )}
    >
      <VoicePlayButton
        asset={asset}
        className={
          onCover
            ? "text-cover-foreground bg-white/20 hover:bg-white/30"
            : "bg-foreground/10 hover:bg-foreground/20"
        }
      />
      <VoiceWave seed={asset.id} active={playing} />
      {duration && <span className="shrink-0">{duration}</span>}
    </span>
  );
}
