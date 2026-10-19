import { Suspense, lazy, useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import { FocusLoader } from "@/components/focus-loader";
import { NODE_META } from "@/constants/canvas";
import { useDirectorStore } from "@/store/director";
import {
  ENGINE_WEIGHT,
  type PreloadDeps,
  type PreloadStage,
  type PreloadState,
  loadProgress,
  loadStage,
  preloadDirector,
  sceneModels,
} from "@/utils/director/preload";

/** 编辑器是懒加载的：three.js 只在进导演台时才下载，不进画布主包 */
const loadEditor = () => import("./director-editor");
const DirectorEditor = lazy(loadEditor);

/** 真实的预加载：编辑器代码块 + 人物模型（figure 也只在懒加载块里） */
const DEPS: PreloadDeps = {
  loadEditor,
  loadModels: async (models, onProgress) => {
    const { loadFigureModels } = await import("@/utils/director/figure");
    return loadFigureModels(models, undefined, onProgress);
  },
};

const STAGE_TEXT: Record<PreloadStage, string> = {
  engine: "正在加载 3D 引擎…",
  models: "正在加载人物模型…",
  ready: "正在进入导演台…",
};

/**
 * 代码块的下载拿不到真实进度，这段时间进度条自己缓慢往前爬，爬到这里为止（要小于引擎的占比），
 * 免得点下去后条一动不动、等到块到了才猛地跳一下
 */
const CREEP_CAP = ENGINE_WEIGHT * 0.8;
const CREEP_TICK_MS = 200;
/** 每一拍往上限靠近的比例 */
const CREEP_RATE = 0.1;

const noop = () => {};

/**
 * 导演台的宿主：点「进入导演台」（会话打开）的那一刻起立刻出加载层，
 * 加载完才挂编辑器，加载层再淡出。整套和进画布的加载一致（FocusLoader），进度显示在加载层里。
 */
export function DirectorHost() {
  const nodeId = useDirectorStore((state) => state.nodeId);
  return nodeId === null ? null : <DirectorSession key={nodeId} />;
}

function DirectorSession() {
  const [models] = useState(() => sceneModels(useDirectorStore.getState().scene));
  const hasModels = models.length > 0;
  const [state, setState] = useState<PreloadState>({ engine: false, models: hasModels ? 0 : 1 });
  const [loaded, setLoaded] = useState(false);
  const [revealed, setRevealed] = useState(false);
  const [creep, setCreep] = useState(0);
  const KindIcon = NODE_META.get("director")?.icon;

  useEffect(() => {
    let alive = true;
    preloadDirector(models, DEPS, (next) => alive && setState(next))
      .then(() => alive && setLoaded(true))
      .catch(() => {
        if (!alive) return;
        toast.error("导演台加载失败，请检查网络后重试");
        useDirectorStore.getState().reset();
      });
    return () => {
      alive = false;
    };
  }, [models]);

  useEffect(() => {
    if (loaded) return;
    const timer = window.setInterval(
      () => setCreep((value) => value + (CREEP_CAP - value) * CREEP_RATE),
      CREEP_TICK_MS,
    );
    return () => window.clearInterval(timer);
  }, [loaded]);

  // 加载期间随时可以按 Esc 放弃：加载好以后 Esc 归编辑器自己处理
  useEffect(() => {
    if (loaded) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.stopPropagation();
      useDirectorStore.getState().reset();
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [loaded]);

  const onDone = useCallback(() => setRevealed(true), []);
  const progress = loaded ? 1 : Math.max(loadProgress(state, hasModels), creep);

  return (
    <>
      {loaded && (
        <Suspense fallback={null}>
          <DirectorEditor />
        </Suspense>
      )}
      {!revealed && (
        // 编辑器始终是深色，加载层跟着深色，淡出后不会闪一下
        <div className="dark">
          <FocusLoader
            icon={KindIcon ? <KindIcon className="size-9" /> : null}
            title="导演台"
            subtitle={STAGE_TEXT[loaded ? "ready" : loadStage(state, hasModels)]}
            label="正在打开导演台"
            progress={progress}
            ready={loaded}
            onOpen={noop}
            onDone={onDone}
          />
        </div>
      )}
    </>
  );
}
