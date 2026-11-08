import { useEffect, useState } from "react";

import {
  PET_LOAD_TIMEOUT_MS,
  petAssetsStatus,
  type PetAssetsStatus,
} from "@/utils/pet/assets-status";
import { buildKeyframes } from "@/utils/pet/atlas";
import {
  PET_CELL,
  parsePetManifest,
  resolvePetRows,
  type PetManifest,
  type PetRow,
} from "@/utils/pet/rows";

/** 加载好的桌宠素材 */
export type PetAssets = {
  skin: string;
  manifest: PetManifest;
  rows: PetRow[];
  hasSleeping: boolean;
  /** 图集地址 */
  sheetUrl: string;
  /** 图集实际像素尺寸，用来设 background-size */
  sheet: { width: number; height: number };
  /** buildKeyframes 生成的 CSS */
  css: string;
};

/** 已经加载好的素材：同一会话里重复挂载不再请求 */
const loaded = new Map<string, PetAssets>();
/** 加载失败的皮肤：本次会话不再重试，一直用胶囊 */
const failed = new Set<string>();

const skinDir = (skin: string) => `${import.meta.env.BASE_URL}pets/${encodeURIComponent(skin)}/`;

function preloadImage(url: string): Promise<{ width: number; height: number }> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => resolve({ width: img.naturalWidth, height: img.naturalHeight });
    img.onerror = () => reject(new Error(`图集加载失败：${url}`));
    img.src = url;
  });
}

async function loadAssets(skin: string): Promise<PetAssets> {
  const dir = skinDir(skin);
  const res = await fetch(`${dir}pet.json`);
  if (!res.ok) throw new Error(`pet.json 请求失败：${res.status}`);
  const manifest = parsePetManifest(await res.json());
  const { rows, hasSleeping } = resolvePetRows(manifest);
  const sheetUrl = new URL(manifest.spritesheetPath, new URL(dir, window.location.href)).toString();
  const sheet = await preloadImage(sheetUrl);
  return {
    skin,
    manifest,
    rows,
    hasSleeping,
    sheetUrl,
    sheet,
    css: buildKeyframes(rows, PET_CELL),
  };
}

/** 正在加载的请求：预取和挂载时的加载共用同一次，不重复下载 */
const inflight = new Map<string, Promise<PetAssets>>();

function loadOnce(skin: string): Promise<PetAssets> {
  const pending = inflight.get(skin);
  if (pending) return pending;
  const next = loadAssets(skin).then(
    (assets) => {
      loaded.set(skin, assets);
      return assets;
    },
    (error: unknown) => {
      failed.add(skin);
      console.warn("桌宠素材加载失败，本次会话退回胶囊按钮", error);
      throw error;
    },
  );
  inflight.set(skin, next);
  return next;
}

/** 进画布页时就开始拉素材，和画布数据并行；失败静默，由桌宠层按失败处理 */
export function prefetchPetAssets(skin: string) {
  if (loaded.has(skin) || failed.has(skin)) return;
  loadOnce(skin).catch(() => {});
}

/**
 * 挂载就加载桌宠素材（pet.json、图集），不等浏览器空闲：素材只有 500KB 左右。
 * status 给外壳用：loading 时胶囊先不画、给桌宠留位；失败或等超过 4 秒退回胶囊，
 * 超时后素材到了仍会换成桌宠。失败时本次会话不再重试，打一条 warn。enabled 为 false（隐藏档）时不加载。
 */
export function usePetAssets(
  skin: string,
  enabled: boolean,
): { assets: PetAssets | null; status: PetAssetsStatus } {
  const [assets, setAssets] = useState<PetAssets | null>(() => loaded.get(skin) ?? null);
  const [failedSkin, setFailedSkin] = useState<string | null>(null);
  const [timedOutSkin, setTimedOutSkin] = useState<string | null>(null);

  useEffect(() => {
    if (!enabled || failed.has(skin) || loaded.has(skin)) return;
    let alive = true;
    const timer = window.setTimeout(() => setTimedOutSkin(skin), PET_LOAD_TIMEOUT_MS);
    loadOnce(skin).then(
      (next) => {
        if (alive) setAssets(next);
      },
      () => {
        if (alive) setFailedSkin(skin);
      },
    );
    return () => {
      alive = false;
      window.clearTimeout(timer);
    };
  }, [skin, enabled]);

  const ready = enabled ? (assets?.skin === skin ? assets : (loaded.get(skin) ?? null)) : null;
  const status = petAssetsStatus({
    enabled,
    ready: ready !== null,
    failed: failed.has(skin) || failedSkin === skin,
    timedOut: timedOutSkin === skin,
  });
  return { assets: ready, status };
}
