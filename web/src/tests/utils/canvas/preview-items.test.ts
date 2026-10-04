import { describe, expect, test } from "bun:test";

import type { CanvasNode, NodeKind } from "@/types";
import {
  collectPreviewItems,
  formatFileSize,
  resolveActiveIndex,
  stepPreviewIndex,
} from "@/utils/canvas/preview-items";

const node = (
  id: string,
  x: number,
  y: number,
  kind: NodeKind = "image",
  src: string | null = `/files/${id}.png`,
  mediaType?: "image" | "video" | "audio",
  height = 270,
): CanvasNode => ({
  id,
  type: "canvas",
  position: { x, y },
  measured: { width: 384, height },
  data: {
    kind,
    label: `节点${id}`,
    src,
    mediaType: mediaType ?? (kind === "video" ? "video" : "image"),
  },
});

describe("collectPreviewItems", () => {
  test("只收有素材的图片和视频，音频、文本、没出结果的节点不进", () => {
    const items = collectPreviewItems([
      node("img", 0, 0),
      node("vid", 400, 0, "video", "/files/v.mp4"),
      node("aud", 800, 0, "audio", "/files/a.mp3", "audio"),
      node("txt", 0, 300, "script", null),
      node("empty", 400, 300, "image", null),
    ]);
    expect(items.map((i) => i.id)).toEqual(["img", "vid"]);
    expect(items[1]?.mediaType).toBe("video");
  });

  test("先按行从上到下，同一行内从左到右；略有错位的节点仍算同一行", () => {
    const items = collectPreviewItems([
      node("c", 800, 20),
      node("d", 0, 400),
      node("a", 0, 0),
      node("b", 400, 60),
    ]);
    expect(items.map((i) => i.id)).toEqual(["a", "b", "c", "d"]);
  });

  test("同一行内错位的节点不会因为 y 略大而排到右边节点后面", () => {
    const items = collectPreviewItems([node("right", 400, 0), node("left", 0, 100)]);
    expect(items.map((i) => i.id)).toEqual(["left", "right"]);
  });

  test("outputs 里选中的版本以 data.src 为准，条目带上节点名与文件名", () => {
    const n = node("a", 0, 0);
    n.data.fileName = "封面.png";
    const [item] = collectPreviewItems([n]);
    expect(item).toMatchObject({
      id: "a",
      src: "/files/a.png",
      label: "节点a",
      fileName: "封面.png",
    });
  });
});

describe("stepPreviewIndex", () => {
  test("到头停住，不循环", () => {
    expect(stepPreviewIndex(0, -1, 3)).toBe(0);
    expect(stepPreviewIndex(2, 1, 3)).toBe(2);
    expect(stepPreviewIndex(1, 1, 3)).toBe(2);
    expect(stepPreviewIndex(1, -1, 3)).toBe(0);
  });
});

describe("resolveActiveIndex", () => {
  const items = collectPreviewItems([node("a", 0, 0), node("b", 400, 0), node("c", 800, 0)]);

  test("当前项还在就原样返回它的位置", () => {
    expect(resolveActiveIndex(items, "b", 1)).toBe(1);
  });

  test("当前项被删了，落到原位置上的相邻项，越界取最后一项", () => {
    const rest = items.filter((i) => i.id !== "b");
    expect(resolveActiveIndex(rest, "b", 1)).toBe(1);
    expect(resolveActiveIndex(rest.slice(0, 1), "b", 1)).toBe(0);
  });

  test("列表空了返回 -1，调用方据此关闭弹层", () => {
    expect(resolveActiveIndex([], "a", 0)).toBe(-1);
  });
});

describe("formatFileSize", () => {
  test("按 1024 进位，MB 及以上保留一位小数，KB 取整", () => {
    expect(formatFileSize(512)).toBe("512 B");
    expect(formatFileSize(850 * 1024)).toBe("850 KB");
    expect(formatFileSize(11.34 * 1024 * 1024)).toBe("11.3 MB");
    expect(formatFileSize(2.5 * 1024 ** 3)).toBe("2.5 GB");
  });

  test("没有有效大小返回 null，信息胶囊就只显示宽高", () => {
    expect(formatFileSize(null)).toBeNull();
    expect(formatFileSize(0)).toBeNull();
    expect(formatFileSize(Number.NaN)).toBeNull();
  });
});
