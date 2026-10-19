import { useState, type ReactNode } from "react";
import { useShallow } from "zustand/react/shallow";
import {
  Box,
  ChevronDown,
  Eye,
  EyeOff,
  Layers,
  SlidersHorizontal,
  Users,
  Video,
  X,
} from "lucide-react";

import { cn } from "@/lib/utils";
import { useDirectorStore, type DirectorAddKind, type ObjectPatch } from "@/store/director";
import type {
  CameraPose,
  CharacterPose,
  DirectorAspect,
  DirectorCamera,
  DirectorCharacter,
  DirectorProp,
  PoseId,
  PropShape,
} from "@/types";
import { ASPECT_RATIO } from "@/utils/director/export";
import { POSE_LABELS, PROP_LABELS, canAdd, limitMessage } from "@/utils/director/scene";

/** 焦距档位（毫米）和对应的垂直视场角（全画幅 24mm 高） */
const LENSES = [24, 35, 50, 85, 135].map((mm) => ({
  mm,
  fov: Math.round(((2 * Math.atan(12 / mm)) / Math.PI) * 180 * 10) / 10,
}));

/** 输入框、下拉共用的样式 */
const FIELD_CLASS =
  "border-input bg-background text-foreground focus-visible:ring-ring h-7 w-full min-w-0 rounded-md border px-2 text-xs outline-none focus-visible:ring-2 disabled:opacity-50";

/**
 * 编辑器右侧面板：「图层」按人物 / 道具 / 机位分组列出对象，「属性」改选中对象的参数。
 * 选中对象后自动切到属性。
 */
export function DirectorSidebar() {
  const [tab, setTab] = useState<"layers" | "props">("layers");
  const selectedId = useDirectorStore((state) => state.selectedId);
  // 新选中一个对象时切到属性页：渲染时对比上一次的选中，不用 effect 再渲染一轮
  const [seen, setSeen] = useState(selectedId);
  if (selectedId !== seen) {
    setSeen(selectedId);
    if (selectedId) setTab("props");
  }

  return (
    <aside className="border-border bg-card flex min-h-0 flex-col border-l">
      <div className="grid grid-cols-2 gap-1 p-2">
        <TabButton active={tab === "layers"} onClick={() => setTab("layers")}>
          <Layers className="size-3.5" />
          图层
        </TabButton>
        <TabButton active={tab === "props"} onClick={() => setTab("props")}>
          <SlidersHorizontal className="size-3.5" />
          属性
        </TabButton>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-2">
        {tab === "layers" ? <LayerList /> : <Properties />}
      </div>
      <AddBar />
    </aside>
  );
}

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "text-muted-foreground hover:text-foreground focus-visible:ring-ring inline-flex h-8 items-center justify-center gap-1.5 rounded-md text-xs font-medium outline-none focus-visible:ring-2",
        active && "bg-muted text-foreground",
      )}
    >
      {children}
    </button>
  );
}

/** 面板底部的添加按钮：到上限时禁用并写原因 */
function AddBar() {
  const scene = useDirectorStore((state) => state.scene);
  const add = (kind: DirectorAddKind) => useDirectorStore.getState().addObject(kind);
  const button = (kind: DirectorAddKind, label: string) => {
    const allowed = !!scene && canAdd(scene, kind);
    return (
      <button
        type="button"
        disabled={!allowed}
        title={allowed ? undefined : limitMessage(kind)}
        onClick={() => add(kind)}
        className="bg-muted text-foreground hover:bg-muted/70 focus-visible:ring-ring h-8 rounded-md text-xs outline-none focus-visible:ring-2 disabled:opacity-40"
      >
        + {label}
      </button>
    );
  };
  return (
    <div className="border-border grid grid-cols-3 gap-1.5 border-t p-2">
      {button("character", "人物")}
      {button("prop", "道具")}
      {button("camera", "机位")}
    </div>
  );
}

/** 图层列表：点行选中，悬停出现显隐和删除 */
function LayerList() {
  const scene = useDirectorStore((state) => state.scene);
  const selectedId = useDirectorStore((state) => state.selectedId);
  if (!scene) return null;
  const group = (
    title: string,
    icon: ReactNode,
    items: (DirectorCharacter | DirectorProp | DirectorCamera)[],
    swatch: (item: DirectorCharacter | DirectorProp | DirectorCamera) => string,
  ) => (
    <div className="mt-1">
      <div className="text-muted-foreground flex h-7 items-center gap-1.5 text-xs">
        <ChevronDown className="size-3.5" />
        {icon}
        {title} ({items.length})
      </div>
      {items.map((item) => {
        const keys = "keys" in item ? item.keys.length : 0;
        const hidden = "hidden" in item && item.hidden;
        return (
          <div
            key={item.id}
            role="button"
            tabIndex={0}
            onClick={() => useDirectorStore.getState().select(item.id)}
            onKeyDown={(event) =>
              event.key === "Enter" && useDirectorStore.getState().select(item.id)
            }
            className={cn(
              "group/row hover:bg-muted/60 focus-visible:ring-ring flex h-8 cursor-pointer items-center gap-2 rounded-md pr-1 pl-6 text-xs outline-none focus-visible:ring-2",
              selectedId === item.id && "bg-muted",
              hidden && "text-muted-foreground",
            )}
          >
            <span className="size-2 shrink-0 rounded-full" style={{ background: swatch(item) }} />
            <span className="truncate">{item.name}</span>
            {keys > 0 && (
              <span className="bg-background text-muted-foreground rounded px-1 text-[10px] tabular-nums">
                {keys} 帧
              </span>
            )}
            <span className="ml-auto flex opacity-0 group-hover/row:opacity-100 group-focus-within/row:opacity-100">
              {!("fov" in item) && (
                <RowAction
                  title={hidden ? "显示" : "隐藏"}
                  onClick={() =>
                    useDirectorStore.getState().updateObject(item.id, { hidden: !hidden })
                  }
                >
                  {hidden ? <EyeOff className="size-3.5" /> : <Eye className="size-3.5" />}
                </RowAction>
              )}
              <RowAction
                title="删除"
                onClick={() => useDirectorStore.getState().removeObject(item.id)}
              >
                <X className="size-3.5" />
              </RowAction>
            </span>
          </div>
        );
      })}
    </div>
  );
  return (
    <>
      {group("人物", <Users className="size-3.5" />, scene.characters, (item) =>
        "color" in item ? item.color : "",
      )}
      {group("道具", <Box className="size-3.5" />, scene.props, () => "var(--muted-foreground)")}
      {group("机位", <Video className="size-3.5" />, scene.cameras, (item) =>
        item.id === scene.activeCameraId ? "var(--foreground)" : "var(--muted-foreground)",
      )}
    </>
  );
}

function RowAction({
  title,
  onClick,
  children,
}: {
  title: string;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      title={title}
      onClick={(event) => {
        event.stopPropagation();
        onClick();
      }}
      className="text-muted-foreground hover:text-foreground focus-visible:ring-ring inline-flex size-6 items-center justify-center rounded outline-none focus-visible:ring-2"
    >
      {children}
    </button>
  );
}

/** 一次性改站位 / 机位状态并算一步撤销（属性面板提交时用） */
const commitTransform = (id: string, patch: ObjectPatch) => {
  const store = useDirectorStore.getState();
  store.startGesture();
  store.transformObject(id, patch);
  store.endGesture();
};

/** 选中对象的属性 */
function Properties() {
  const scene = useDirectorStore((state) => state.scene);
  const selectedId = useDirectorStore((state) => state.selectedId);
  // 订阅播放头和临时姿态：站位字段显示的是播放头时刻的样子
  // poseOf 每次返回新对象，按字段浅比较，免得每次读都当成变化
  const pose = useDirectorStore(
    useShallow((state) => (selectedId ? (state.poseOf(selectedId) ?? null) : null)),
  );
  const pending = useDirectorStore((state) => (selectedId ? !!state.pending[selectedId] : false));
  const t = useDirectorStore((state) => state.t);
  if (!scene || !selectedId) {
    return (
      <p className="text-muted-foreground mt-2 text-xs">
        在视口或图层里选中一个对象，这里显示它的参数。
      </p>
    );
  }
  const character = scene.characters.find((c) => c.id === selectedId);
  const prop = scene.props.find((p) => p.id === selectedId);
  const camera = scene.cameras.find((c) => c.id === selectedId);
  const update = (patch: Record<string, unknown>) =>
    useDirectorStore.getState().updateObject(selectedId, patch);
  const item = character ?? prop ?? camera;
  if (!item) return null;

  const keyHint = (keys: number) => (
    <p
      className={cn(
        "mt-3 text-[11px] leading-relaxed",
        pending ? "text-status-running" : "text-muted-foreground",
      )}
    >
      {keys > 0
        ? pending
          ? `未记录：按 K 把改动写到 ${t.toFixed(2)}s 的关键帧`
          : `已有 ${keys} 个关键帧：在这里改是临时姿态，按 K 写到 ${t.toFixed(2)}s`
        : "没有关键帧：这里改的是静态站位"}
    </p>
  );

  return (
    <div className="space-y-2 pt-1">
      <Row label="名称">
        <TextField
          key={item.id}
          value={item.name}
          onCommit={(name) => update({ name: name.slice(0, 20) })}
        />
      </Row>
      {character && (
        <>
          <Row label="姿势">
            <select
              className={FIELD_CLASS}
              value={character.pose}
              onChange={(event) => update({ pose: event.target.value as PoseId })}
            >
              {(Object.keys(POSE_LABELS) as PoseId[]).map((id) => (
                <option key={id} value={id}>
                  {POSE_LABELS[id]}
                </option>
              ))}
            </select>
          </Row>
          <Row label="颜色">
            <input
              type="color"
              value={character.color}
              onChange={(event) => update({ color: event.target.value })}
              className="h-7 w-12 cursor-pointer rounded border-0 bg-transparent"
            />
          </Row>
          <NumberRow
            label="位置 X"
            unit="m"
            step={0.05}
            value={(pose as CharacterPose).x}
            onCommit={(x) => commitTransform(item.id, { x })}
          />
          <NumberRow
            label="位置 Z"
            unit="m"
            step={0.05}
            value={(pose as CharacterPose).z}
            onCommit={(z) => commitTransform(item.id, { z })}
          />
          <NumberRow
            label="朝向"
            unit="°"
            step={1}
            value={(pose as CharacterPose).yaw}
            onCommit={(yaw) => commitTransform(item.id, { yaw })}
          />
          <NumberRow
            label="体型"
            step={0.05}
            min={0.8}
            max={1.2}
            value={character.scale}
            onCommit={(scale) => update({ scale })}
          />
          <p className="text-muted-foreground text-[11px]">姿势在一段镜头内固定，不进时间轴。</p>
          {keyHint(character.keys.length)}
        </>
      )}
      {prop && (
        <>
          <Row label="形状">
            <select
              className={FIELD_CLASS}
              value={prop.shape}
              onChange={(event) => update({ shape: event.target.value as PropShape })}
            >
              {(Object.keys(PROP_LABELS) as PropShape[]).map((shape) => (
                <option key={shape} value={shape}>
                  {PROP_LABELS[shape]}
                </option>
              ))}
            </select>
          </Row>
          <NumberRow
            label="位置 X"
            unit="m"
            step={0.05}
            value={prop.x}
            onCommit={(x) => commitTransform(prop.id, { x })}
          />
          <NumberRow
            label="位置 Z"
            unit="m"
            step={0.05}
            value={prop.z}
            onCommit={(z) => commitTransform(prop.id, { z })}
          />
          <NumberRow
            label="旋转"
            unit="°"
            step={1}
            value={prop.yaw}
            onCommit={(yaw) => commitTransform(prop.id, { yaw })}
          />
          <NumberRow
            label="宽"
            unit="m"
            step={0.1}
            min={0.05}
            max={20}
            value={prop.w}
            onCommit={(w) => commitTransform(prop.id, { w })}
          />
          <NumberRow
            label="高"
            unit="m"
            step={0.1}
            min={0.05}
            max={10}
            value={prop.h}
            onCommit={(h) => commitTransform(prop.id, { h })}
          />
          <NumberRow
            label="深"
            unit="m"
            step={0.1}
            min={0.02}
            max={20}
            value={prop.d}
            onCommit={(d) => commitTransform(prop.id, { d })}
          />
          <p className="text-muted-foreground mt-3 text-[11px]">道具不打关键帧。</p>
        </>
      )}
      {camera && (
        <>
          <Row label="画幅">
            <select
              className={FIELD_CLASS}
              value={camera.aspect}
              onChange={(event) => update({ aspect: event.target.value as DirectorAspect })}
            >
              {(Object.keys(ASPECT_RATIO) as DirectorAspect[]).map((aspect) => (
                <option key={aspect} value={aspect}>
                  {aspect}
                </option>
              ))}
            </select>
          </Row>
          <Row label="焦距">
            <select
              className={FIELD_CLASS}
              value=""
              onChange={(event) => {
                const fov = Number(event.target.value);
                if (fov) commitTransform(camera.id, { fov });
              }}
            >
              <option value="">选择镜头…</option>
              {LENSES.map((lens) => (
                <option key={lens.mm} value={lens.fov}>
                  {lens.mm}mm（FOV {lens.fov}°）
                </option>
              ))}
            </select>
          </Row>
          <Row label="看向">
            <select
              className={FIELD_CLASS}
              value={camera.lookAt ?? ""}
              onChange={(event) => update({ lookAt: event.target.value || undefined })}
            >
              <option value="">无</option>
              {scene.characters.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </Row>
          <NumberRow
            label="FOV"
            unit="°"
            step={1}
            min={10}
            max={120}
            value={(pose as CameraPose).fov}
            onCommit={(fov) => commitTransform(camera.id, { fov })}
          />
          <NumberRow
            label="位置 X"
            unit="m"
            step={0.05}
            value={(pose as CameraPose).x}
            onCommit={(x) => commitTransform(camera.id, { x })}
          />
          <NumberRow
            label="高度"
            unit="m"
            step={0.05}
            min={0.1}
            value={(pose as CameraPose).y}
            onCommit={(y) => commitTransform(camera.id, { y })}
          />
          <NumberRow
            label="位置 Z"
            unit="m"
            step={0.05}
            value={(pose as CameraPose).z}
            onCommit={(z) => commitTransform(camera.id, { z })}
          />
          <NumberRow
            label="偏航"
            unit="°"
            step={1}
            value={(pose as CameraPose).yaw}
            onCommit={(yaw) => commitTransform(camera.id, { yaw })}
          />
          <NumberRow
            label="俯仰"
            unit="°"
            step={1}
            min={-85}
            max={85}
            value={(pose as CameraPose).pitch}
            onCommit={(pitch) => commitTransform(camera.id, { pitch })}
          />
          {keyHint(camera.keys.length)}
        </>
      )}
    </div>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="grid grid-cols-[56px_minmax(0,1fr)] items-center gap-2 text-xs">
      <span className="text-muted-foreground">{label}</span>
      {children}
    </label>
  );
}

/** 文本框：失焦或回车才提交，空白退回原值；没在编辑时显示最新的值 */
function TextField({ value, onCommit }: { value: string; onCommit: (value: string) => void }) {
  const [draft, setDraft] = useState<string | null>(null);
  const commit = () => {
    const next = draft?.trim();
    setDraft(null);
    if (next && next !== value) onCommit(next);
  };
  return (
    <input
      className={FIELD_CLASS}
      value={draft ?? value}
      maxLength={20}
      onFocus={() => setDraft(value)}
      onChange={(event) => setDraft(event.target.value)}
      onBlur={commit}
      onKeyDown={(event) => event.key === "Enter" && (event.target as HTMLInputElement).blur()}
    />
  );
}

/**
 * 数字框：失焦或回车才提交（一次提交算一步撤销），超出范围夹回去；
 * 没在编辑时跟着播放头、拖动实时刷新。
 */
function NumberRow({
  label,
  value,
  unit,
  step,
  min,
  max,
  onCommit,
}: {
  label: string;
  value: number;
  unit?: string;
  step: number;
  min?: number;
  max?: number;
  onCommit: (value: number) => void;
}) {
  const decimals = step < 0.1 ? 2 : step < 1 ? 1 : 0;
  const shown = value.toFixed(decimals);
  const [draft, setDraft] = useState<string | null>(null);
  const commit = () => {
    if (draft === null) return;
    const parsed = Number(draft);
    setDraft(null);
    if (!Number.isFinite(parsed)) return;
    const clamped = Math.min(max ?? Infinity, Math.max(min ?? -Infinity, parsed));
    if (clamped.toFixed(decimals) !== shown) onCommit(clamped);
  };
  return (
    <Row label={label}>
      <span className="flex items-center gap-1.5">
        <input
          type="number"
          inputMode="decimal"
          step={step}
          min={min}
          max={max}
          className={cn(FIELD_CLASS, "tabular-nums")}
          value={draft ?? shown}
          onFocus={() => setDraft(shown)}
          onChange={(event) => setDraft(event.target.value)}
          onBlur={commit}
          onKeyDown={(event) => event.key === "Enter" && (event.target as HTMLInputElement).blur()}
        />
        {unit && <span className="text-muted-foreground w-3 text-[11px]">{unit}</span>}
      </span>
    </Row>
  );
}
