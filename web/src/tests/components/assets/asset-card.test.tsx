import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { AssetDto } from "@/api/asset/type";
import type { GeneratedItemDto, LibraryItemDto } from "@/api/asset-library/type";
import { HistoryAssetCard, LibraryAssetCard } from "@/components/assets/asset-card";
import { UploadCard } from "@/components/assets/upload-card";
import { formatAssetDay } from "@/utils/assets/format";

const asset = (overrides: Partial<AssetDto> = {}): AssetDto => ({
  id: "12",
  kind: "image",
  url: "/files/g/a.png",
  mimeType: "image/png",
  byteSize: 1,
  width: 1024,
  height: 1024,
  durationMs: null,
  fileName: "a.png",
  ...overrides,
});

const generated = (overrides: Partial<GeneratedItemDto> = {}): GeneratedItemDto => ({
  asset: asset(),
  prompt: "雪地里的旧楼，黄昏",
  modelLabel: "Seedream 4",
  canvas: null,
  createdAt: "2026-10-14T10:20:00Z",
  libraryItemId: null,
  ...overrides,
});

const noop = () => {};
const history = (item: GeneratedItemDto) =>
  renderToStaticMarkup(
    <HistoryAssetCard item={item} onPreview={noop} onAddToLibrary={noop} onOpenCanvas={noop} />,
  );

describe("HistoryAssetCard：生成历史卡片", () => {
  test("没加入资产库时没有「已在资产库」角标", () => {
    expect(history(generated())).not.toContain("已在资产库");
  });

  test("library_item_id 非空时显示「已在资产库」角标", () => {
    const out = history(generated({ libraryItemId: "a1" }));
    expect(out).toContain("已在资产库");
    expect(out).toContain('data-in-library="true"');
  });

  test("悬停说明里有完整提示词和「模型 · 日期」", () => {
    const out = history(generated());
    expect(out).toContain("雪地里的旧楼，黄昏");
    expect(out).toContain("Seedream 4 · ");
  });

  test("图片封面用 ?v=thumb，视频封面用 ?v=poster，并带时长角标", () => {
    expect(history(generated())).toContain("v=thumb");
    const video = history(
      generated({
        asset: asset({
          kind: "video",
          url: "/files/g/a.mp4",
          durationMs: 65_000,
          fileName: "a.mp4",
        }),
      }),
    );
    expect(video).toContain("v=poster");
    expect(video).toContain("1:05");
  });

  test("音频没有封面，也没有时长以外的画面请求", () => {
    const out = history(generated({ asset: asset({ kind: "audio", url: "/files/g/a.mp3" }) }));
    expect(out).not.toContain("v=thumb");
    expect(out).not.toContain("v=poster");
  });

  test("没有任何可做的操作（已在库里且来自首页对话）时不出现 ⋯ 菜单", () => {
    expect(history(generated({ libraryItemId: "a1" }))).not.toContain("更多操作");
    expect(history(generated())).toContain("更多操作");
  });
});

describe("LibraryAssetCard：个人资产库卡片", () => {
  const item: LibraryItemDto = {
    id: "i1",
    asset: asset({ fileName: "角色正面.png" }),
    folderId: null,
    createdAt: "2026-10-14T10:20:00Z",
  };

  test("常显文件名，并有 ⋯ 菜单", () => {
    const out = renderToStaticMarkup(
      <LibraryAssetCard item={item} onPreview={noop} onMove={noop} onRemove={noop} />,
    );
    expect(out).toContain("角色正面.png");
    expect(out).toContain("更多操作");
  });
});

describe("UploadCard：上传中的卡片", () => {
  const task = {
    id: "u1",
    file: new File(["x"], "ref.png"),
    folderId: null,
    status: "uploading" as const,
    percent: 40,
    error: null,
  };

  test("显示文件名、进度和取消入口", () => {
    const out = renderToStaticMarkup(<UploadCard task={task} onCancel={noop} onRetry={noop} />);
    expect(out).toContain("ref.png");
    expect(out).toContain("40%");
    expect(out).toContain("取消上传");
  });

  test("失败时显示原因和「重试」，不再显示进度条", () => {
    const out = renderToStaticMarkup(
      <UploadCard
        task={{ ...task, status: "failed", error: "文件超过大小上限" }}
        onCancel={noop}
        onRetry={noop}
      />,
    );
    expect(out).toContain("文件超过大小上限");
    expect(out).toContain("重试");
    expect(out).not.toContain("progressbar");
  });
});

describe("formatAssetDay：卡片日期", () => {
  const now = new Date(2026, 9, 16);

  test("今年只写月日，往年带年份", () => {
    expect(formatAssetDay(new Date(2026, 9, 14, 12).toISOString(), now)).toBe("10月14日");
    expect(formatAssetDay(new Date(2025, 11, 3, 12).toISOString(), now)).toBe("2025年12月3日");
  });

  test("解析不了时原样返回", () => {
    expect(formatAssetDay("oops", now)).toBe("oops");
  });
});
