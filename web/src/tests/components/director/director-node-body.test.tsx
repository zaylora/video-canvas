import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { DirectorNodeBody } from "@/components/director/director-node-body";
import type { DirectorScene } from "@/types";
import { createDefaultScene } from "@/utils/director/scene";

const scene = (thumb?: boolean): DirectorScene => {
  let n = 0;
  const base = createDefaultScene(() => `id${++n}`);
  return thumb ? { ...base, thumb: { assetId: "a1", url: "/thumb.png" } } : base;
};

const render = (value: DirectorScene | undefined) =>
  renderToStaticMarkup(<DirectorNodeBody scene={value} onEnter={() => {}} />);

describe("导演台节点正文", () => {
  test("还没有缩略图：说明文字加居中的「进入导演台」按钮，没有黑色蒙版", () => {
    const out = render(scene());
    expect(out).toContain("在 3D 空间中设计角色、机位与镜头");
    expect(out).toContain("进入导演台");
    expect(out).toContain("<button");
    expect(out).not.toContain("<img");
    expect(out).not.toContain("data-mask");
  });

  test("有缩略图：缩略图当背景，盖黑色蒙版，居中仍有进入按钮，说明文字不再出现", () => {
    const out = render(scene(true));
    expect(out).toContain('src="/thumb.png"');
    expect(out).toContain("data-mask");
    expect(out).toContain("进入导演台");
    expect(out).not.toContain("在 3D 空间中设计角色、机位与镜头");
  });

  test("机位标签留在底部，有缩略图时也在", () => {
    for (const value of [scene(), scene(true)]) {
      const out = render(value);
      expect(out).toContain("机位1");
      expect(out).toContain("5.0s");
    }
  });

  test("进入按钮带 nodrag：点它不会变成拖动节点", () => {
    expect(render(scene())).toContain("nodrag");
    expect(render(scene(true))).toContain("nodrag");
  });

  test("节点上不再显示「编辑中」", () => {
    expect(render(scene(true))).not.toContain("编辑中");
  });

  test("场景缺失（旧数据）也能画出空态", () => {
    const out = render(undefined);
    expect(out).toContain("进入导演台");
  });
});
