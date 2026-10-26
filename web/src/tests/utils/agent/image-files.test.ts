import { describe, expect, test } from "bun:test";

import { MAX_ATTACH_IMAGES, pickImageFiles } from "@/utils/agent/image-files";

const file = (name: string, type: string) => new File(["x"], name, { type });

describe("pickImageFiles：从粘贴 / 拖入的文件里挑出图片", () => {
  test("只留图片，保持原来的顺序", () => {
    const got = pickImageFiles([
      file("a.png", "image/png"),
      file("story.txt", "text/plain"),
      file("b.jpg", "image/jpeg"),
    ]);
    expect(got.images.map((f) => f.name)).toEqual(["a.png", "b.jpg"]);
    expect(got.skipped).toBe(1);
    expect(got.clipped).toBe(0);
  });

  test("没有图片：返回空，调用方据此放行默认粘贴（文字照常粘进去）", () => {
    const got = pickImageFiles([file("a.md", "text/markdown")]);
    expect(got.images).toEqual([]);
    expect(got.skipped).toBe(1);
  });

  test("空列表和 null 都当作没有文件", () => {
    expect(pickImageFiles([]).images).toEqual([]);
    expect(pickImageFiles(null).images).toEqual([]);
  });

  test(`一次最多 ${MAX_ATTACH_IMAGES} 张，多出来的记在 clipped`, () => {
    const many = Array.from({ length: MAX_ATTACH_IMAGES + 3 }, (_, i) =>
      file(`${i}.png`, "image/png"),
    );
    const got = pickImageFiles(many);
    expect(got.images).toHaveLength(MAX_ATTACH_IMAGES);
    expect(got.images[0].name).toBe("0.png");
    expect(got.clipped).toBe(3);
  });
});
