import { useEffect, useRef, useState } from "react";
import { motion } from "motion/react";
import { useReactFlow } from "@xyflow/react";
import { X } from "lucide-react";
import { toast } from "sonner";

import { UPLOAD_SIZE_LIMIT } from "@/constants/canvas";
import { useCanvasHistoryContext } from "@/hooks/use-canvas-history";
import { DURATION, EASE_OUT, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useDirectorStore } from "@/store/director";
import type { CanvasEdge, CanvasNode, DirectorScene } from "@/types";
import { buildDirectorExportNodes, type DirectorExportItem } from "@/utils/canvas/director-export";
import { uploadRunner } from "@/utils/canvas/upload-runner";
import {
  ASPECT_RATIO,
  exportCameras,
  exportFileName,
  exportSize,
  type CameraExportResult,
  type ExportResolution,
} from "@/utils/director/export";
import {
  UNSUPPORTED_EXPORT_MESSAGE,
  canExportVideo,
  createExportEngine,
} from "@/utils/director/export-engine";
import { frameCount } from "@/utils/director/scene";

/** 码率粗估（字节 / 像素 / 秒），只用来显示「预计大小」 */
const BYTES_PER_PIXEL_SECOND = 0.45;

type Phase = "idle" | "running" | "done";

/**
 * 导出对话框（设计稿 docs/design/3D导演台 §6.6）：选机位、分辨率、要不要首帧图，
 * 逐帧离线渲染编码，完成后在导演台右侧建视频 / 首帧图节点并上传。
 * 画在编辑器里面（不走 Portal），跟着编辑器的深色主题；导出中锁住，只能取消。
 */
export function DirectorExportDialog({
  nodeId,
  onClose,
  onBackToCanvas,
}: {
  nodeId: string;
  onClose: () => void;
  onBackToCanvas: () => void;
}) {
  const { getNode, getNodes, setNodes, setEdges, updateNodeData } = useReactFlow<
    CanvasNode,
    CanvasEdge
  >();
  const history = useCanvasHistoryContext();
  const scene = useDirectorStore((state) => state.scene);
  const [cameraIds, setCameraIds] = useState<string[]>(() =>
    scene?.activeCameraId ? [scene.activeCameraId] : scene?.cameras[0] ? [scene.cameras[0].id] : [],
  );
  const [resolution, setResolution] = useState<ExportResolution>("720p");
  const [withFirstFrame, setWithFirstFrame] = useState(true);
  const [supported, setSupported] = useState<boolean | null>(null);
  const [phase, setPhase] = useState<Phase>("idle");
  /** 每个机位渲染到第几帧 */
  const [progress, setProgress] = useState<Record<string, number>>({});
  /** 失败的机位和原因，可以单独重试 */
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [exported, setExported] = useState(0);
  const controller = useRef<AbortController | null>(null);

  useEffect(() => {
    let alive = true;
    void canExportVideo().then((ok) => alive && setSupported(ok));
    return () => {
      alive = false;
    };
  }, []);
  // 编辑器被关掉（删节点、保存冲突、离开画布）时中止还在跑的导出
  useEffect(() => () => controller.current?.abort(), []);

  if (!scene) return null;
  const running = phase === "running";
  const total = frameCount(scene.duration);
  const chosen = scene.cameras.filter((c) => cameraIds.includes(c.id));
  const estimateMb = chosen.reduce((sum, camera) => {
    const { width, height } = exportSize(camera.aspect, resolution);
    return sum + (width * height * BYTES_PER_PIXEL_SECOND * scene.duration) / 1024 / 1024;
  }, 0);

  /** 成功的导出物放回画布：记一步画布撤销，建节点和来源线，交给上传流程 */
  const place = (snapshot: DirectorScene, results: Extract<CameraExportResult, { ok: true }>[]) => {
    const source = getNode(nodeId);
    if (!source) {
      toast("导演台节点已不存在，导出结果没有放回画布");
      return 0;
    }
    const files: File[] = [];
    const items: DirectorExportItem[] = [];
    for (const result of results) {
      const camera = snapshot.cameras.find((c) => c.id === result.cameraId)!;
      const aspect = ASPECT_RATIO[camera.aspect];
      const video = exportFileName(source.data.label, camera.name, "mp4");
      items.push({ kind: "video", cameraName: camera.name, aspect, fileName: video });
      files.push(new File([result.video], video, { type: "video/mp4" }));
      if (result.firstFrame) {
        const image = exportFileName(`${source.data.label}_首帧`, camera.name, "png");
        items.push({ kind: "image", cameraName: camera.name, aspect, fileName: image });
        files.push(new File([result.firstFrame], image, { type: "image/png" }));
      }
    }
    history?.record();
    const built = buildDirectorExportNodes({ source, nodes: getNodes(), items });
    setNodes((nodes) => [...nodes, ...built.nodes]);
    setEdges((edges) => [...edges, ...built.edges]);
    built.nodes.forEach((node, index) => {
      void uploadRunner
        .start(node.id, files[index], (id, patch) => updateNodeData(id, patch))
        .then((outcome) => {
          if (outcome === "failed") toast.error(`「${node.data.label}」上传失败，可以在节点上重试`);
        });
    });
    return results.length;
  };

  const run = async (ids: string[]) => {
    const snapshot = useDirectorStore.getState().scene;
    if (!snapshot || ids.length === 0) return;
    const abort = new AbortController();
    controller.current = abort;
    setPhase("running");
    useDirectorStore.getState().setExporting(true);
    setProgress((prev) => ({ ...prev, ...Object.fromEntries(ids.map((id) => [id, 0])) }));
    setErrors((prev) =>
      Object.fromEntries(Object.entries(prev).filter(([id]) => !ids.includes(id))),
    );
    try {
      const results = await exportCameras(
        {
          scene: snapshot,
          cameraIds: ids,
          resolution,
          withFirstFrame,
          signal: abort.signal,
          onProgress: (id, done) => setProgress((prev) => ({ ...prev, [id]: done })),
        },
        createExportEngine(),
      );
      const ok: Extract<CameraExportResult, { ok: true }>[] = [];
      const failed: Record<string, string> = {};
      for (const result of results) {
        if (!result.ok) failed[result.cameraId] = result.error;
        else if (result.video.size > UPLOAD_SIZE_LIMIT.video) {
          failed[result.cameraId] = "视频超过上传大小上限，请降低分辨率或缩短时长";
        } else ok.push(result);
      }
      const placed = ok.length ? place(snapshot, ok) : 0;
      setExported((n) => n + placed);
      setErrors((prev) => ({ ...prev, ...failed }));
      const failures = Object.keys(failed).length;
      if (placed)
        toast.success(`已导出 ${placed} 段视频到画布${failures ? `，${failures} 个机位失败` : ""}`);
      else if (failures) toast.error("导出失败，可以重试失败的机位");
      setPhase(failures ? "idle" : "done");
    } catch (error) {
      if ((error as Error)?.name !== "AbortError") throw error;
      toast("已取消导出，已编码的内容已丢弃");
      setProgress({});
      setPhase("idle");
    } finally {
      controller.current = null;
      useDirectorStore.getState().setExporting(false);
    }
  };

  const toggleCamera = (id: string) =>
    setCameraIds((ids) => (ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id]));
  const locked = running || phase === "done";
  const blocked =
    supported === false
      ? UNSUPPORTED_EXPORT_MESSAGE
      : chosen.length === 0
        ? "至少选一台机位"
        : null;

  return (
    <div className="absolute inset-0 z-20 grid place-items-center bg-black/50">
      <motion.div
        role="dialog"
        aria-modal
        aria-label="导出运镜预览视频"
        initial={{ opacity: 0, scale: 0.98 }}
        animate={{ opacity: 1, scale: 1 }}
        transition={{ duration: DURATION.slow, ease: EASE_OUT }}
        className="bg-card border-border text-foreground w-[520px] max-w-[calc(100%-32px)] rounded-xl border p-5 shadow-xl"
      >
        <div className="flex items-center">
          <h3 className="text-base font-semibold">导出运镜预览视频</h3>
          {!running && (
            <button
              type="button"
              title="关闭"
              onClick={onClose}
              className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring ml-auto inline-flex size-7 items-center justify-center rounded-md outline-none focus-visible:ring-2"
            >
              <X className="size-4" />
            </button>
          )}
        </div>
        <p className="text-muted-foreground mt-1 text-xs leading-relaxed">
          每个机位导出一段 MP4（H.264，24fps，{scene.duration.toFixed(1)}s，{total}{" "}
          帧），画面不带标签、网格和控件。 完成后在画布上导演台右侧新建节点并上传。
        </p>

        <div className="mt-4 text-xs font-medium">机位</div>
        <div className="mt-1.5 space-y-1.5">
          {scene.cameras.map((camera) => {
            const on = cameraIds.includes(camera.id);
            const done = progress[camera.id];
            const error = errors[camera.id];
            const { width, height } = exportSize(camera.aspect, resolution);
            return (
              <label
                key={camera.id}
                className={cn(
                  "border-border flex items-center gap-2.5 rounded-lg border px-3 py-2 text-xs",
                  on && "border-foreground/40",
                  !locked && "cursor-pointer",
                )}
              >
                <input
                  type="checkbox"
                  checked={on}
                  disabled={locked}
                  onChange={() => toggleCamera(camera.id)}
                  className="accent-foreground"
                />
                <span className="min-w-0 flex-1">
                  <span className="font-medium">{camera.name}</span>
                  <span className="text-muted-foreground ml-2 tabular-nums">
                    {camera.aspect} · {width}×{height} ·{" "}
                    {camera.keys.length ? `${camera.keys.length} 个关键帧` : "固定机位"}
                  </span>
                  {done !== undefined && (
                    <span className="mt-1.5 block">
                      <span className="bg-muted block h-1.5 overflow-hidden rounded-full">
                        <span
                          className={cn("block h-full", error ? "bg-destructive" : "bg-foreground")}
                          style={{ width: `${(done / total) * 100}%` }}
                        />
                      </span>
                      <span
                        className={cn(
                          "mt-1 block tabular-nums",
                          error ? "text-destructive" : "text-muted-foreground",
                        )}
                      >
                        {error ?? (done >= total ? "完成" : `渲染 ${done}/${total} 帧`)}
                      </span>
                    </span>
                  )}
                </span>
                {error && !running && (
                  <button
                    type="button"
                    onClick={(event) => {
                      event.preventDefault();
                      void run([camera.id]);
                    }}
                    className="bg-muted hover:bg-muted/70 h-7 shrink-0 rounded-md px-2.5"
                  >
                    重试该机位
                  </button>
                )}
              </label>
            );
          })}
        </div>

        <div className="mt-4 flex flex-wrap items-center gap-4 text-xs">
          <span className="font-medium">分辨率</span>
          {(["720p", "1080p"] as const).map((value) => (
            <label key={value} className="flex items-center gap-1.5">
              <input
                type="radio"
                name="director-resolution"
                checked={resolution === value}
                disabled={locked}
                onChange={() => setResolution(value)}
                className="accent-foreground"
              />
              {value}
            </label>
          ))}
          <label className="flex items-center gap-1.5">
            <input
              type="checkbox"
              checked={withFirstFrame}
              disabled={locked}
              onChange={(event) => setWithFirstFrame(event.target.checked)}
              className="accent-foreground"
            />
            同时导出首帧图
          </label>
        </div>

        <p className="text-muted-foreground mt-3 text-[11px] tabular-nums">
          {supported === false
            ? UNSUPPORTED_EXPORT_MESSAGE
            : running
              ? "导出中，请保持页面在前台"
              : `预计约 ${estimateMb.toFixed(1)}MB（粗估）· 逐帧离线渲染，不依赖实时播放`}
        </p>

        <div className="mt-4 flex justify-end gap-2">
          {running ? (
            <DialogButton onClick={() => controller.current?.abort()}>取消导出</DialogButton>
          ) : phase === "done" ? (
            <>
              <DialogButton onClick={onBackToCanvas}>回到画布查看</DialogButton>
              <DialogButton primary onClick={onClose}>
                继续编辑
              </DialogButton>
            </>
          ) : (
            <>
              <DialogButton onClick={onClose}>{exported ? "完成" : "取消"}</DialogButton>
              <DialogButton
                primary
                disabled={!!blocked || supported === null}
                title={blocked ?? undefined}
                onClick={() => void run(cameraIds.filter((id) => !progress[id] || errors[id]))}
              >
                开始导出（{chosen.length}）
              </DialogButton>
            </>
          )}
        </div>
      </motion.div>
    </div>
  );
}

function DialogButton({
  primary,
  disabled,
  title,
  onClick,
  children,
}: {
  primary?: boolean;
  disabled?: boolean;
  title?: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <motion.button
      type="button"
      whileTap={disabled ? undefined : TAP}
      disabled={disabled}
      title={title}
      onClick={onClick}
      className={cn(
        "focus-visible:ring-ring h-8 rounded-md px-3 text-xs font-medium outline-none focus-visible:ring-2 disabled:opacity-40",
        primary
          ? "bg-primary text-primary-foreground hover:bg-primary/85"
          : "bg-muted hover:bg-muted/70",
      )}
    >
      {children}
    </motion.button>
  );
}
