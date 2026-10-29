import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router";

import { UploadCard } from "@/pages/drama/upload-card";

const html = () =>
  renderToStaticMarkup(
    <MemoryRouter>
      <UploadCard />
    </MemoryRouter>,
  );

describe("短剧创作卡：上传和粘贴二选一", () => {
  test("顶部是「上传剧本 / 小说」「粘贴正文」两个 tab，默认选中上传", () => {
    const out = html();
    expect(out).toContain('role="tablist"');
    expect(out).toContain("上传剧本 / 小说");
    expect(out).toContain("粘贴正文");
    expect(out).toMatch(/aria-selected="true"[^>]*>[\s\S]*?上传剧本 \/ 小说/);
  });

  test("两块面板叠放，没选中的那块不可交互，读屏也跳过", () => {
    const out = html();
    expect(out).toMatch(/data-pane="upload"(?![^>]*inert)/);
    expect(out).toMatch(/data-pane="paste"[^>]*aria-hidden="true"/);
    expect(out).toMatch(/data-pane="paste"[^>]*inert/);
  });

  test("上传区是居中的一摞稿纸 + 一句拖入提示 + 格式限制", () => {
    const out = html();
    expect(out).toContain('data-slot="source-papers"');
    expect(out).toContain("拖入剧本文件，或");
    expect(out).toContain("选择文件");
    expect(out).toContain("txt · md · docx");
    expect(out).toContain("30 万字以内");
  });

  test("创作卡是半透明胶囊面板", () => {
    const out = html();
    expect(out).toContain('data-slot="drama-upload-card"');
    expect(out).toContain("bg-chrome");
    expect(out).toContain("backdrop-blur-xl");
  });
});
