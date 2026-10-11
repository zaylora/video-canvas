import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { MediaPreview } from "@/components/canvas/media-preview";

const SRC = "/files/abc/a.png";

/** 数 img 标签里 src 命中某段文字的个数 */
const imgCount = (out: string, needle: string) =>
  [...out.matchAll(/<img[^>]*\ssrc="([^"]*)"/g)].filter((match) => match[1].includes(needle))
    .length;

describe("MediaPreview 的 full 模式（不依赖画布缩放）", () => {
  test("缩略图和原图两层都渲染：缩略图先出，原图加载完再叠上去", () => {
    const out = renderToStaticMarkup(<MediaPreview src={SRC} mode="full" />);
    expect(imgCount(out, "v=thumb")).toBe(1);
    // 原图层：地址不带 v 参数
    expect([...out.matchAll(/<img[^>]*\ssrc="([^"]*)"/g)].map((match) => match[1])).toContain(SRC);
    expect(out).not.toContain("<video");
  });

  test("两层都没加载出来时盖着灰色扫光占位", () => {
    const out = renderToStaticMarkup(<MediaPreview src={SRC} mode="full" />);
    expect(out).toContain('data-slot="media-skeleton"');
  });

  test("thumb 模式只请求缩略图，不叠原图", () => {
    const out = renderToStaticMarkup(<MediaPreview src={SRC} mode="thumb" />);
    expect(imgCount(out, "a.png")).toBe(1);
    expect(imgCount(out, "v=thumb")).toBe(1);
  });

  test("外链没有缩略图变体，直接看原图", () => {
    const out = renderToStaticMarkup(
      <MediaPreview src="https://cdn.example.com/a.png" mode="full" />,
    );
    expect(imgCount(out, "cdn.example.com")).toBe(1);
    expect(imgCount(out, "v=thumb")).toBe(0);
  });
});
