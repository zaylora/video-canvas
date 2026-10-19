import type { DirectorScene, FigureModel } from "@/types";

/**
 * 进入导演台前的预加载（设计稿 docs/design/3D导演台 §6.3）：
 * 编辑器代码块（带着 three.js）和场景里用到的人台模型同时开始下载，都到了再挂编辑器，
 * 进入的过程里用加载层（和进画布同款）显示进度。
 * 这个文件不能引入 three：它在画布主包里，three 只能在懒加载块里。
 */

/** 编辑器代码块在总进度里占的比例，剩下的是人物模型 */
export const ENGINE_WEIGHT = 0.6;

/** 预加载进行到哪了 */
export type PreloadState = {
  /** 编辑器代码块（3D 引擎）到了没有 */
  engine: boolean;
  /** 人物模型下载解析的进度，0 ~ 1；没有模型要加载时直接是 1 */
  models: number;
};

/** 预加载的阶段，加载层按它换说明文字 */
export type PreloadStage = "engine" | "models" | "ready";

/** 预加载要用到的两个加载函数，由调用方注入（真实的动态 import 在 director-host 里，测试里换成假的） */
export type PreloadDeps = {
  /** 加载编辑器代码块；失败就抛出 */
  loadEditor: () => Promise<unknown>;
  /** 加载人物模型并上报 0 ~ 1 的进度；失败返回 false，不抛错 */
  loadModels: (
    models: readonly FigureModel[],
    onProgress: (fraction: number) => void,
  ) => Promise<boolean>;
};

const clamp01 = (value: number) => Math.min(1, Math.max(0, value));

/**
 * 总进度，0 ~ 1。引擎占 ENGINE_WEIGHT，人物模型占剩下的；没有人物时引擎就是全部。
 * @param hasModels 场景里有没有要加载的人物模型
 */
export function loadProgress(state: PreloadState, hasModels: boolean): number {
  if (!hasModels) return state.engine ? 1 : 0;
  return (state.engine ? ENGINE_WEIGHT : 0) + (1 - ENGINE_WEIGHT) * clamp01(state.models);
}

/** 当前处在哪个阶段：先引擎，再人物模型，最后就绪 */
export function loadStage(state: PreloadState, hasModels: boolean): PreloadStage {
  if (!state.engine) return "engine";
  if (hasModels && state.models < 1) return "models";
  return "ready";
}

/** 场景里用到的人物模型（去重）：没有场景或没有人物时是空的，缺省模型按男性 */
export function sceneModels(scene: DirectorScene | null): FigureModel[] {
  if (!scene) return [];
  return [...new Set(scene.characters.map((c) => c.model ?? "male"))];
}

/**
 * 预加载导演台：引擎和人物模型并行，进度只增不减。
 * @param onProgress 每次有进展就调用，参数是当前状态的快照
 * @returns 人物模型是否全部加载成功；失败不算失败，编辑器里人物先用简化形体、并提示重试
 * @throws 编辑器代码块加载失败（断网、发版后旧块被清掉等），调用方提示并退出
 */
export async function preloadDirector(
  models: readonly FigureModel[],
  deps: PreloadDeps,
  onProgress: (state: PreloadState) => void,
): Promise<boolean> {
  const state: PreloadState = { engine: false, models: models.length ? 0 : 1 };
  const emit = () => onProgress({ ...state });
  emit();

  const engine = deps.loadEditor().then(() => {
    state.engine = true;
    emit();
  });
  const figures = models.length
    ? deps
        .loadModels(models, (fraction) => {
          state.models = Math.max(state.models, clamp01(fraction));
          emit();
        })
        .then((ok) => {
          state.models = 1;
          emit();
          return ok;
        })
        // 引擎失败时 Promise.all 已经拒绝了，这里的失败不能再变成未处理的拒绝
        .catch(() => false)
    : Promise.resolve(true);

  const [, modelsOk] = await Promise.all([engine, figures]);
  return modelsOk;
}
