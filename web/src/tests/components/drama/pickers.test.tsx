import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { renderToStaticMarkup } from "react-dom/server";

import type { DramaStyles } from "@/api/drama/type";
import { StyleGrid } from "@/components/drama/pickers";
import { CUSTOM_STYLE_KEY } from "@/utils/drama/options";

const catalog: DramaStyles = JSON.parse(
  readFileSync(
    new URL("../../../../../backend/internal/drama/styles.json", import.meta.url),
    "utf8",
  ),
);

const renderGrid = (styles = catalog.styles, value = "real_city_film") =>
  renderToStaticMarkup(
    <StyleGrid
      styles={styles}
      value={value}
      custom=""
      onPick={() => {}}
      onCustomChange={() => {}}
    />,
  );

describe("画风选择器预览图", () => {
  test("已生成的画风展示对应图片，其余保留占位、名称、提示词和选中态", async () => {
    const html = renderGrid();
    let imageCount = 0;
    for (const style of catalog.styles) {
      const exists = await Bun.file(
        new URL(`../../../../public/drama-styles/${style.key}.webp`, import.meta.url),
      ).exists();
      if (exists) {
        expect(html).toContain(`src="/drama-styles/${style.key}.webp"`);
        expect(html).toContain(`alt="${style.name}风格预览"`);
        imageCount += 1;
      } else {
        expect(html).not.toContain(`src="/drama-styles/${style.key}.webp"`);
      }
      expect(html).toContain(`title="${style.prompt}"`);
      expect(html).toContain(style.name);
    }
    expect(html.match(/<img /g)).toHaveLength(imageCount);
    expect(html).toMatch(/aria-pressed="true"[^>]*title="以当代都市真人电影/);
  });

  test("所有内置预览都是实际存在的 WebP 图片，适合缩略图加载", async () => {
    for (const style of catalog.styles) {
      const file = Bun.file(
        new URL(`../../../../public/drama-styles/${style.key}.webp`, import.meta.url),
      );
      expect(await file.exists()).toBe(true);
      const bytes = new Uint8Array(await file.arrayBuffer());
      expect(new TextDecoder().decode(bytes.slice(0, 4))).toBe("RIFF");
      expect(new TextDecoder().decode(bytes.slice(8, 12))).toBe("WEBP");
      expect(bytes.length).toBeLessThan(200_000);
    }
  });

  test("新增未知画风保留占位，自定义仍显示描述框且不请求不存在的图片", () => {
    const html = renderGrid(
      [{ key: "future_style", name: "未来画风", prompt: "新的画风描述" }],
      CUSTOM_STYLE_KEY,
    );
    expect(html).not.toContain("<img ");
    expect(html).toContain('data-slot="drama-poster"');
    expect(html).toContain("未来画风");
    expect(html).toContain("自定义");
    expect(html).toContain("<textarea");
  });
});
