import { useRef, useState } from "react";
import { Diamond, Pause, Play, SkipBack, Trash2, UserRound, Video } from "lucide-react";
import { toast } from "sonner";

import { cn } from "@/lib/utils";
import { useDirectorStore } from "@/store/director";
import { CAMERA_PRESETS, type CameraPresetId } from "@/utils/director/preset";
import { DIRECTOR_FPS, DURATION_RANGE } from "@/utils/director/scene";

/** 时长下拉的选项：1–15 秒，0.5 一档 */
const DURATIONS = Array.from(
  { length: (DURATION_RANGE.max - DURATION_RANGE.min) / DURATION_RANGE.step + 1 },
  (_, i) => DURATION_RANGE.min + i * DURATION_RANGE.step,
);

/** 轨道行高、刻度行高（像素） */
const ROW = 28;
const RULER = 24;

const FIELD_CLASS =
  "border-input bg-background text-foreground focus-visible:ring-ring h-7 rounded-md border px-2 text-xs outline-none focus-visible:ring-2 disabled:opacity-50";

/**
 * 编辑器底部的时间轴：播放控制、时长、记录关键帧、运镜预设，
 * 下面每个机位、每个人物一条轨道。点刻度或拖播放头跳转，拖关键帧改时间，右键关键帧切换插值。
 */
export function DirectorTimeline() {
  const scene = useDirectorStore((state) => state.scene);
  const t = useDirectorStore((state) => state.t);
  const playing = useDirectorStore((state) => state.playing);
  const selectedId = useDirectorStore((state) => state.selectedId);
  const selectedKey = useDirectorStore((state) => state.selectedKey);
  const pending = useDirectorStore((state) => state.pending);
  const [presetId, setPresetId] = useState<CameraPresetId>("dollyIn");
  const preset = CAMERA_PRESETS.find((p) => p.id === presetId)!;
  const [strength, setStrength] = useState(preset.defaultStrength);
  const lanesRef = useRef<HTMLDivElement>(null);
  if (!scene) return null;

  const store = () => useDirectorStore.getState();
  const isCamera = !!selectedId && scene.cameras.some((c) => c.id === selectedId);
  const keyable = isCamera || (!!selectedId && scene.characters.some((c) => c.id === selectedId));
  const rows = [
    ...scene.cameras.map((item) => ({ item, icon: <Video className="size-3.5" /> })),
    ...scene.characters.map((item) => ({ item, icon: <UserRound className="size-3.5" /> })),
  ];
  const pct = (time: number) => `${(time / scene.duration) * 100}%`;
  /** 指针横坐标换成时间 */
  const timeAt = (clientX: number) => {
    const rect = lanesRef.current?.getBoundingClientRect();
    if (!rect || rect.width === 0) return 0;
    return Math.min(1, Math.max(0, (clientX - rect.left) / rect.width)) * scene.duration;
  };

  /** 在空白处按下：跳到那里并可以拖着走 */
  const scrub = (event: React.PointerEvent) => {
    if (event.button !== 0) return;
    store().selectKey(null);
    store().setTime(timeAt(event.clientX));
    const move = (e: PointerEvent) => store().setTime(timeAt(e.clientX));
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };

  /** 按下关键帧：选中并跳过去；拖动松手后改时间（算一步） */
  const grabKey = (event: React.PointerEvent, id: string, keyTime: number) => {
    if (event.button !== 0) return;
    event.stopPropagation();
    store().selectKey({ id, t: keyTime });
    store().setTime(keyTime);
    let target = keyTime;
    const element = event.currentTarget as HTMLElement;
    const move = (e: PointerEvent) => {
      target = Math.round(timeAt(e.clientX) * 100) / 100;
      element.style.left = pct(target);
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      if (Math.abs(target - keyTime) >= 0.01) {
        store().moveKey(id, keyTime, target);
        store().setTime(target);
      }
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };

  const runPreset = () => {
    if (!isCamera || !selectedId) return;
    const { warning } = store().applyPreset(
      selectedId,
      presetId,
      preset.unit ? strength : undefined,
    );
    if (warning) toast(warning);
    else toast.success(`已套用「${preset.label}」，按空格播放`);
  };

  return (
    <section className="border-border bg-card grid min-h-0 grid-rows-[44px_minmax(0,1fr)] border-t">
      <div className="border-border flex items-center gap-1.5 overflow-x-auto border-b px-2">
        <IconButton
          title={playing ? "暂停（空格）" : "播放（空格）"}
          onClick={() => store().setPlaying(!playing)}
        >
          {playing ? <Pause className="size-4" /> : <Play className="size-4" />}
        </IconButton>
        <IconButton title="回到开头" onClick={() => store().setTime(0)}>
          <SkipBack className="size-4" />
        </IconButton>
        <span className="min-w-28 text-xs tabular-nums">
          F{Math.round(t * DIRECTOR_FPS)} · {t.toFixed(2)}s / {scene.duration.toFixed(1)}s
        </span>
        <label className="text-muted-foreground flex shrink-0 items-center gap-1 text-xs">
          时长
          <select
            className={FIELD_CLASS}
            value={scene.duration}
            onChange={(event) => store().setDuration(Number(event.target.value))}
          >
            {DURATIONS.map((d) => (
              <option key={d} value={d}>
                {d}s
              </option>
            ))}
          </select>
        </label>
        <Divider />
        <button
          type="button"
          disabled={!keyable}
          title={keyable ? "把选中对象此刻的样子记成关键帧（K）" : "先选中一个人物或机位"}
          onClick={() => selectedId && store().recordKey(selectedId)}
          className="bg-muted text-foreground hover:bg-muted/70 focus-visible:ring-ring inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md px-2.5 text-xs outline-none focus-visible:ring-2 disabled:opacity-40"
        >
          <Diamond className="size-3.5" />
          记录关键帧
        </button>
        <Divider />
        <select
          className={FIELD_CLASS}
          value={presetId}
          disabled={!isCamera}
          title={isCamera ? preset.description : "先选中一台机位"}
          onChange={(event) => {
            const next = CAMERA_PRESETS.find((p) => p.id === event.target.value)!;
            setPresetId(next.id);
            setStrength(next.defaultStrength);
          }}
        >
          {CAMERA_PRESETS.map((p) => (
            <option key={p.id} value={p.id}>
              运镜：{p.label}
            </option>
          ))}
        </select>
        {preset.unit && (
          <label className="text-muted-foreground flex shrink-0 items-center gap-1 text-xs">
            <input
              type="number"
              className={cn(FIELD_CLASS, "w-16 tabular-nums")}
              disabled={!isCamera}
              min={preset.min}
              max={preset.max}
              step={preset.step}
              value={strength}
              onChange={(event) => {
                const value = Number(event.target.value);
                if (Number.isFinite(value))
                  setStrength(Math.min(preset.max, Math.max(preset.min, value)));
              }}
            />
            {preset.unit}
          </label>
        )}
        <button
          type="button"
          disabled={!isCamera}
          onClick={runPreset}
          className="bg-muted text-foreground hover:bg-muted/70 focus-visible:ring-ring h-7 shrink-0 rounded-md px-2.5 text-xs outline-none focus-visible:ring-2 disabled:opacity-40"
        >
          套用
        </button>
        {selectedKey && (
          <button
            type="button"
            title="删除选中的关键帧（Delete）"
            onClick={() => store().deleteSelectedKey()}
            className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring ml-auto inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md px-2 text-xs outline-none focus-visible:ring-2"
          >
            <Trash2 className="size-3.5" />
            删除关键帧
          </button>
        )}
      </div>

      <div className="grid min-h-0 grid-cols-[148px_minmax(0,1fr)] overflow-y-auto">
        <div className="border-border border-r">
          <div style={{ height: RULER }} />
          {rows.map(({ item, icon }) => (
            <button
              key={item.id}
              type="button"
              onClick={() => store().select(item.id)}
              className={cn(
                "text-muted-foreground hover:text-foreground flex w-full items-center gap-1.5 truncate px-2.5 text-xs",
                selectedId === item.id && "text-foreground",
              )}
              style={{ height: ROW }}
            >
              {icon}
              <span className="truncate">{item.name}</span>
            </button>
          ))}
        </div>
        <div ref={lanesRef} className="relative cursor-pointer select-none" onPointerDown={scrub}>
          <div
            className="border-border text-muted-foreground relative border-b text-[10px] tabular-nums"
            style={{ height: RULER }}
          >
            {Array.from({ length: Math.floor(scene.duration) + 1 }, (_, s) => (
              <span key={s} className="absolute top-1 -translate-x-1/2" style={{ left: pct(s) }}>
                {s}s
              </span>
            ))}
          </div>
          {rows.map(({ item }) => (
            <div
              key={item.id}
              className={cn(
                "border-border/50 relative border-b",
                selectedId === item.id && "bg-muted/40",
              )}
              style={{ height: ROW }}
            >
              {item.keys.map((key) => {
                const chosen =
                  selectedKey?.id === item.id && Math.abs(selectedKey.t - key.t) < 0.005;
                return (
                  <span
                    key={key.t}
                    title={`${key.t.toFixed(2)}s · ${key.ease === "linear" ? "线性" : "缓入缓出"}（右键切换）`}
                    onPointerDown={(event) => grabKey(event, item.id, key.t)}
                    onContextMenu={(event) => {
                      event.preventDefault();
                      store().toggleKeyEase(item.id, key.t);
                    }}
                    className={cn(
                      "absolute top-1/2 size-2.5 -translate-x-1/2 -translate-y-1/2 rotate-45 cursor-grab rounded-[2px]",
                      chosen ? "bg-status-running" : "bg-foreground",
                      key.ease === "linear" && !chosen && "bg-muted-foreground",
                    )}
                    style={{ left: pct(key.t) }}
                  />
                );
              })}
              {pending[item.id] && (
                <span
                  title="未记录，按 K 写入"
                  className="border-status-running pointer-events-none absolute top-1/2 size-2.5 -translate-x-1/2 -translate-y-1/2 rotate-45 rounded-[2px] border border-dashed"
                  style={{ left: pct(t) }}
                />
              )}
            </div>
          ))}
          <div
            className="bg-foreground pointer-events-none absolute top-0 bottom-0 w-px"
            style={{ left: pct(t) }}
          >
            <span className="bg-foreground absolute -top-0 left-1/2 size-2.5 -translate-x-1/2 rounded-full" />
          </div>
          {rows.every(({ item }) => item.keys.length === 0) && (
            <p className="text-muted-foreground pointer-events-none absolute top-10 left-4 text-xs">
              选中机位，从上方选一个运镜预设点「套用」；或拖动对象后按 K 记录关键帧
            </p>
          )}
        </div>
      </div>
    </section>
  );
}

function IconButton({
  title,
  onClick,
  children,
}: {
  title: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      className="text-foreground hover:bg-muted focus-visible:ring-ring inline-flex size-7 shrink-0 items-center justify-center rounded-md outline-none focus-visible:ring-2"
    >
      {children}
    </button>
  );
}

function Divider() {
  return <span className="bg-border mx-0.5 h-5 w-px shrink-0" />;
}
