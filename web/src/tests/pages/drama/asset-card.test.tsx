import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { CanvasNodeData } from "@/types/canvas";
import { AssetCard } from "@/pages/drama/workspace/asset-card";
import type { AssetItem } from "@/utils/drama/assets";

const item = (overrides: Partial<AssetItem> = {}): AssetItem => ({
  id: "n1",
  type: "character",
  name: "林薇",
  look: "",
  label: "林薇",
  episodes: [1],
  status: "idle",
  data: { kind: "character" } as CanvasNodeData,
  ...overrides,
});

const noop = () => {};
const card = (value: AssetItem, multi = false) =>
  renderToStaticMarkup(
    <AssetCard
      item={value}
      index={0}
      multi={multi}
      selected={false}
      onToggle={noop}
      onLocate={noop}
    />,
  );

describe("AssetCard：短剧资产卡", () => {
  test("整张卡就是进画布的入口，没有悬停的「生成」「画布」按钮", () => {
    const out = card(item());
    expect(out).toContain('aria-label="在画布中打开 林薇"');
    expect(out).not.toContain(">生成<");
    expect(out).not.toContain(">画布<");
  });

  test("生成失败也不出「重试」按钮", () => {
    expect(card(item({ status: "error" }))).not.toContain(">重试<");
  });

  test("封面图完整显示不裁切：16:9 的三视图放进 3:4 的卡片里也能看全", () => {
    const out = card(
      item({
        status: "done",
        data: { kind: "character", src: "https://cdn/sheet.png" } as CanvasNodeData,
      }),
    );
    expect(out).toContain("object-contain");
    expect(out).not.toContain("object-cover");
  });

  test("多选模式下点卡片是勾选，不进画布", () => {
    const out = card(item(), true);
    expect(out).toContain('aria-label="选择"');
    expect(out).not.toContain("在画布中打开");
  });
});
