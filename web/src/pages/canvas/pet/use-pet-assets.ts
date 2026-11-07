import { useEffect, useState } from "react";

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

/** 等浏览器空闲再做，最多等 2 秒；不支持 requestIdleCallback 的退回短延时 */
function whenIdle(task: () => void): () => void {
  if (typeof window.requestIdleCallback === "function") {
    const id = window.requestIdleCallback(task, { timeout: 2000 });
    return () => window.cancelIdleCallback(id);
  }
  const id = window.setTimeout(task, 200);
  return () => window.clearTimeout(id);
}

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

/**
 * 画布空闲后加载桌宠素材（pet.json、图集），没加载好前是 null，页面显示原来的胶囊。
 * 失败时本次会话不再重试，打一条 warn。enabled 为 false（隐藏档）时不加载。
 */
export function usePetAssets(skin: string, enabled: boolean): PetAssets | null {
  const [assets, setAssets] = useState<PetAssets | null>(() => loaded.get(skin) ?? null);

  useEffect(() => {
    if (!enabled || failed.has(skin)) return;
    if (loaded.has(skin)) return;
    let alive = true;
    const cancelIdle = whenIdle(() => {
      loadAssets(skin).then(
        (next) => {
          loaded.set(skin, next);
          if (alive) setAssets(next);
        },
        (error: unknown) => {
          failed.add(skin);
          console.warn("桌宠素材加载失败，本次会话退回胶囊按钮", error);
        },
      );
    });
    return () => {
      alive = false;
      cancelIdle();
    };
  }, [skin, enabled]);

  if (!enabled) return null;
  return assets?.skin === skin ? assets : (loaded.get(skin) ?? null);
}
