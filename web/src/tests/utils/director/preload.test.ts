import { describe, expect, test } from "bun:test";

import type { FigureModel } from "@/types";
import {
  ENGINE_WEIGHT,
  type PreloadDeps,
  type PreloadState,
  loadProgress,
  loadStage,
  preloadDirector,
  sceneModels,
} from "@/utils/director/preload";
import { createDefaultScene, addCharacter } from "@/utils/director/scene";

const deferred = <T = void>() => {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
};

describe("加载进度", () => {
  test("什么都没到是 0，全部到齐是 1", () => {
    expect(loadProgress({ engine: false, models: 0 }, true)).toBe(0);
    expect(loadProgress({ engine: true, models: 1 }, true)).toBeCloseTo(1, 10);
  });

  test("引擎占 ENGINE_WEIGHT，人物模型占剩下的，按模型进度线性增长", () => {
    expect(loadProgress({ engine: true, models: 0 }, true)).toBeCloseTo(ENGINE_WEIGHT, 10);
    expect(loadProgress({ engine: false, models: 1 }, true)).toBeCloseTo(1 - ENGINE_WEIGHT, 10);
    expect(loadProgress({ engine: true, models: 0.5 }, true)).toBeCloseTo(
      ENGINE_WEIGHT + (1 - ENGINE_WEIGHT) / 2,
      10,
    );
  });

  test("场景里没有人物时只看引擎，不会卡在模型那一半", () => {
    expect(loadProgress({ engine: false, models: 0 }, false)).toBe(0);
    expect(loadProgress({ engine: true, models: 0 }, false)).toBe(1);
  });

  test("模型进度越界会被夹住", () => {
    expect(loadProgress({ engine: true, models: 3 }, true)).toBeCloseTo(1, 10);
    expect(loadProgress({ engine: false, models: -1 }, true)).toBe(0);
  });

  test("阶段：先引擎，再人物模型，最后就绪", () => {
    expect(loadStage({ engine: false, models: 0 }, true)).toBe("engine");
    expect(loadStage({ engine: true, models: 0.4 }, true)).toBe("models");
    expect(loadStage({ engine: true, models: 1 }, true)).toBe("ready");
    expect(loadStage({ engine: true, models: 0 }, false)).toBe("ready");
  });
});

describe("场景里用到的人物模型", () => {
  test("去重，缺省按男性", () => {
    let n = 0;
    let scene = createDefaultScene(() => `id${++n}`);
    expect(sceneModels(scene)).toEqual(["male"]);
    scene = addCharacter(scene, () => "f1", "female").scene;
    scene = addCharacter(scene, () => "f2", "female").scene;
    expect([...sceneModels(scene)].sort()).toEqual(["female", "male"]);
  });

  test("没有场景或没有人物就没有模型要加载", () => {
    expect(sceneModels(null)).toEqual([]);
    let n = 0;
    const scene = createDefaultScene(() => `id${++n}`);
    expect(sceneModels({ ...scene, characters: [] })).toEqual([]);
  });
});

describe("预加载导演台", () => {
  test("引擎和人物模型同时开始加载，都好了才算完成", async () => {
    const engine = deferred();
    const models = deferred<boolean>();
    const calls: string[] = [];
    const deps: PreloadDeps = {
      loadEditor: () => {
        calls.push("editor");
        return engine.promise;
      },
      loadModels: (list) => {
        calls.push(`models:${list.join(",")}`);
        return models.promise;
      },
    };
    let settled = false;
    const done = preloadDirector(["male"], deps, () => {}).then((ok) => {
      settled = true;
      return ok;
    });
    expect(calls).toEqual(["editor", "models:male"]);

    engine.resolve();
    await Promise.resolve();
    await Promise.resolve();
    expect(settled).toBe(false);

    models.resolve(true);
    expect(await done).toBe(true);
  });

  test("进度只增不减，最后是引擎和模型都到齐", async () => {
    const seen: PreloadState[] = [];
    const deps: PreloadDeps = {
      loadEditor: async () => {},
      loadModels: async (_list, onProgress) => {
        onProgress(0.3);
        onProgress(0.1); // 乱序或回退的上报不能让进度往回走
        onProgress(0.8);
        return true;
      },
    };
    await preloadDirector(["male"], deps, (state) => seen.push(state));
    const models = seen.map((s) => s.models);
    expect(models).toEqual([...models].sort((a, b) => a - b));
    expect(seen.at(-1)).toEqual({ engine: true, models: 1 });
    expect(seen[0]).toEqual({ engine: false, models: 0 });
  });

  test("没有人物时不去读模型", async () => {
    let called = false;
    const deps: PreloadDeps = {
      loadEditor: async () => {},
      loadModels: async () => {
        called = true;
        return true;
      },
    };
    const seen: PreloadState[] = [];
    expect(await preloadDirector([], deps, (s) => seen.push(s))).toBe(true);
    expect(called).toBe(false);
    expect(seen.at(-1)).toEqual({ engine: true, models: 1 });
  });

  test("人物模型加载失败不算失败：返回 false，编辑器里用简化形体顶着", async () => {
    const deps: PreloadDeps = {
      loadEditor: async () => {},
      loadModels: async () => false,
    };
    expect(await preloadDirector(["male"], deps, () => {})).toBe(false);
  });

  test("编辑器代码块加载失败就抛出，由调用方提示", async () => {
    const deps: PreloadDeps = {
      loadEditor: () => Promise.reject(new Error("chunk 加载失败")),
      loadModels: async () => true,
    };
    await expect(preloadDirector(["male"], deps, () => {})).rejects.toThrow("chunk 加载失败");
  });

  test("引擎失败后模型那边再出错也不会冒出未处理的拒绝", async () => {
    const models = deferred<boolean>();
    const deps: PreloadDeps = {
      loadEditor: () => Promise.reject(new Error("断网")),
      loadModels: () => models.promise,
    };
    const result = preloadDirector(["male" as FigureModel], deps, () => {});
    await expect(result).rejects.toThrow("断网");
    models.reject(new Error("也断了"));
    await Promise.resolve();
  });
});
