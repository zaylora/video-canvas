import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { AttachmentTray } from "@/pages/canvas/agent/attachment-tray";
import type { TrayItem } from "@/utils/agent/attachments";

const noop = () => undefined;
const item = (over: Partial<TrayItem> = {}): TrayItem => ({
  id: "t1",
  name: "参考图.png",
  previewUrl: "blob:preview-1",
  status: "done",
  progress: 100,
  asset: { id: "101", url: "/files/a.png", width: 800, height: 600, mimeType: "image/png" },
  ...over,
});
const html = (items: TrayItem[], vision = true) =>
  renderToStaticMarkup(
    <AttachmentTray items={items} vision={vision} onRetry={noop} onRemove={noop} />,
  );

describe("附件托盘", () => {
  test("托盘空着：什么都不渲染（不占输入框的高度）", () => {
    expect(html([])).toBe("");
  });

  test("已上传：显示本地预览缩略图，有移除按钮", () => {
    const out = html([item()]);
    expect(out).toContain('src="blob:preview-1"');
    expect(out).toContain('aria-label="移除 参考图.png"');
    expect(out).not.toContain("重试上传");
  });

  test("上传中：显示进度，不能重试，仍能移除（取消上传）", () => {
    const out = html([item({ status: "uploading", progress: 40, asset: undefined })]);
    expect(out).toContain('aria-label="上传中 40%"');
    expect(out).toContain('aria-label="移除 参考图.png"');
    expect(out).not.toContain("重试上传");
  });

  test("上传失败：描边提示，点图上的按钮重试；失败原因放在悬停提示里", () => {
    const out = html([item({ status: "error", asset: undefined, error: "网络中断" })]);
    expect(out).toContain('aria-label="重试上传"');
    expect(out).toContain("网络中断");
    expect(out).toContain("border-status-warning");
  });

  test("当前模型不能看图：缩略图变暗，托盘底部写明原因", () => {
    const out = html([item()], false);
    expect(out).toContain("当前模型看不到图片，换一个能看图的模型或移除图片");
    expect(out).toContain("opacity-45");
    expect(html([item()], true)).not.toContain("当前模型看不到图片");
  });

  test("多张按顺序排开", () => {
    const out = html([
      item({ id: "a", name: "一.png", previewUrl: "blob:1" }),
      item({ id: "b", name: "二.png", previewUrl: "blob:2" }),
    ]);
    expect(out.indexOf("blob:1")).toBeLessThan(out.indexOf("blob:2"));
  });

  describe("文档附件：文件卡片", () => {
    const doc = (over: Partial<TrayItem> = {}): TrayItem => ({
      id: "d1",
      name: "_剧本原文.txt",
      kind: "document",
      size: 11_683,
      previewUrl: "",
      status: "done",
      progress: 100,
      asset: { id: "201", url: "/files/s.txt", width: null, height: null, mimeType: "text/plain" },
      ...over,
    });

    test("显示文件名、「TXT · 11.41KB」和移除按钮，不渲染缩略图", () => {
      const out = html([doc()]);
      expect(out).toContain("_剧本原文.txt");
      expect(out).toContain("TXT · 11.41KB");
      expect(out).toContain('aria-label="移除 _剧本原文.txt"');
      expect(out).not.toContain("<img");
    });

    test("markdown 文件显示 MD", () => {
      expect(html([doc({ name: "故事.md", size: 800 })])).toContain("MD · 800B");
    });

    test("上传中：卡片上显示进度；失败：描边并能重试", () => {
      expect(html([doc({ status: "uploading", progress: 40, asset: undefined })])).toContain(
        'aria-label="上传中 40%"',
      );
      const failed = html([doc({ status: "error", asset: undefined, error: "网络中断" })]);
      expect(failed).toContain('aria-label="重试上传"');
      expect(failed).toContain("border-status-warning");
    });

    test("模型不能看图时，文档卡片不变暗，托盘底部也不提示看不到图片", () => {
      const out = html([doc()], false);
      expect(out).not.toContain("opacity-45");
      expect(out).not.toContain("当前模型看不到图片");
    });

    test("图片和文档混排：按顺序，提示只因为图片出现", () => {
      const out = html([item({ previewUrl: "blob:p" }), doc()], false);
      expect(out.indexOf("blob:p")).toBeLessThan(out.indexOf("_剧本原文.txt"));
      expect(out).toContain("当前模型看不到图片");
    });
  });
});
