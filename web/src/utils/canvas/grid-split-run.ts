import type { CanvasNodeData } from "@/types";

import { cutTile, planTiles, type Tile } from "./grid-split";
import { gridTileFileName } from "./grid-split-node";
import { loadSourceImage } from "./outpaint-compose";
import type { SourceImage } from "./outpaint-run";
import type { UploadOutcome } from "./upload-runner";

/** 同时在切图、上传的格数上限：25 格不要一口气全发出去，也不要同时占 25 份解码后的内存 */
export const GRID_UPLOAD_CONCURRENCY = 4;

/** 切分流程里的外部依赖：测试里全换成假的 */
export type GridSplitDeps = {
  /** 读并解码原图；失败要抛 Error，文案可直接显示给用户 */
  load: (src: string) => Promise<SourceImage>;
  /** 切出一格，导出 PNG */
  cut: (image: CanvasImageSource, tile: Tile) => Promise<Blob>;
  /** 把一格的文件传给对应节点，返回收场方式 */
  upload: (nodeId: string, file: File) => Promise<UploadOutcome>;
  /** 节点还在等着这次切分的结果吗；被用户删掉（占位已清）就不再切、不再传 */
  isActive: (nodeId: string) => boolean;
  /** 把改动写回节点 */
  patch: (nodeId: string, patch: Partial<CanvasNodeData>) => void;
  /** 放弃这个节点的占位（这一格切不出来，不会再有上传了） */
  release: (nodeId: string) => void;
};

/** 一次切分怎么收场 */
export type GridSplitResult =
  /** 原图读不出来：一格都没切，节点该撤掉 */
  | { status: "unreadable"; error: Error }
  /** 走完了：failed 是切图或上传失败的格数（被取消的不算），失败的节点留在原地 */
  | { status: "done"; failed: number };

const defaultDeps: Omit<GridSplitDeps, "upload" | "isActive" | "patch" | "release"> = {
  load: async (src) => {
    const bitmap = await loadSourceImage(src);
    return {
      image: bitmap,
      width: bitmap.width,
      height: bitmap.height,
      close: () => bitmap.close(),
    };
  },
  cut: (image, tile) => cutTile(image, tile),
};

/**
 * 默认的外部依赖：读图、切图用真实实现，其余（上传、占位、写节点）由画布侧传入。
 * @param rest 上传、是否仍有效、写回节点、放弃占位
 */
export const gridSplitDeps = (
  rest: Pick<GridSplitDeps, "upload" | "isActive" | "patch" | "release">,
): GridSplitDeps => ({ ...defaultDeps, ...rest });

/**
 * 宫格切分的执行：读原图，按真实像素重新算一遍切分方案，然后一格一格切图并上传。
 * 节点在点确认时就已经按估算的画幅建好了，这里只负责把真实画幅、进度和结果写回它们：
 * 读图失败整体放弃（由调用方撤掉节点）；单格切图失败把那个节点标成失败；单格上传失败留给节点上的重试。
 * 同时最多处理 GRID_UPLOAD_CONCURRENCY 格。
 * @param input.src 原图地址
 * @param input.xs 竖线位置（0 到 1）
 * @param input.ys 横线位置（0 到 1）
 * @param input.nodeIds 每格对应的节点，按行优先，和 planTiles 的顺序一致
 * @param deps 外部依赖
 * @param concurrency 并发上限
 */
export async function runGridSplit(
  input: { src: string; xs: number[]; ys: number[]; nodeIds: string[] },
  deps: GridSplitDeps,
  concurrency: number = GRID_UPLOAD_CONCURRENCY,
): Promise<GridSplitResult> {
  let loaded: SourceImage;
  try {
    loaded = await deps.load(input.src);
  } catch (error) {
    return {
      status: "unreadable",
      error: error instanceof Error ? error : new Error("无法读取原图"),
    };
  }

  try {
    const tiles = planTiles(input.xs, input.ys, loaded.width, loaded.height);
    // 节点按估算的画幅建的，读到真实像素之后校正
    tiles.forEach((tile, index) => {
      const id = input.nodeIds[index];
      if (id !== undefined) deps.patch(id, { mediaAspect: tile.w / tile.h });
    });

    let failed = 0;
    let next = 0;
    const worker = async () => {
      while (next < tiles.length) {
        const index = next++;
        const id = input.nodeIds[index];
        if (id === undefined || !deps.isActive(id)) continue;
        const tile = tiles[index];
        let file: File;
        try {
          const blob = await deps.cut(loaded.image, tile);
          file = new File([blob], gridTileFileName(tile.row, tile.col), { type: "image/png" });
        } catch (error) {
          failed++;
          deps.release(id);
          deps.patch(id, {
            status: "error",
            error: error instanceof Error ? error.message : "切图失败",
            uploadProgress: undefined,
          });
          continue;
        }
        // 切图期间节点可能被删了
        if (!deps.isActive(id)) continue;
        if ((await deps.upload(id, file)) === "failed") failed++;
      }
    };
    await Promise.all(
      Array.from({ length: Math.max(1, Math.min(concurrency, tiles.length)) }, worker),
    );
    return { status: "done", failed };
  } finally {
    loaded.close?.();
  }
}
