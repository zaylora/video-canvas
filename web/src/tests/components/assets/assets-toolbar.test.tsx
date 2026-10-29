import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { AssetMasonry } from "@/components/assets/asset-grid";
import { AssetsToolbar } from "@/components/assets/assets-toolbar";

describe("资产页工具条", () => {
  test("是一条半透明胶囊，内容放在里面", () => {
    const out = renderToStaticMarkup(
      <AssetsToolbar>
        <button type="button">图片</button>
      </AssetsToolbar>,
    );
    expect(out).toContain('data-slot="assets-toolbar"');
    expect(out).toContain("bg-chrome");
    expect(out).toContain("rounded-full");
    expect(out).toContain("图片");
  });

  test("首屏没有滚动，不出现顶栏里的收起胶囊", () => {
    const out = renderToStaticMarkup(
      <AssetsToolbar>
        <span>筛选</span>
      </AssetsToolbar>,
    );
    expect(out).not.toContain('data-slot="assets-toolbar-mini"');
    expect(out.split("筛选").length - 1).toBe(1);
  });
});

describe("资产页瀑布流容器", () => {
  test("按列排，卡片不被拆到两列", () => {
    const out = renderToStaticMarkup(
      <AssetMasonry>
        <div>a</div>
      </AssetMasonry>,
    );
    expect(out).toContain('data-slot="asset-masonry"');
    expect(out).toContain("columns-2");
    expect(out).toContain("break-inside-avoid");
  });
});
