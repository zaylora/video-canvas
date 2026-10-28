import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { AgentAttachmentDto } from "@/api/agent/type";
import { MessageAttachments } from "@/pages/canvas/agent/message-attachments";

const att = (id: string, over: Partial<AgentAttachmentDto> = {}): AgentAttachmentDto => ({
  asset_id: id,
  name: `图片 ${id}.png`,
  mime_type: "image/png",
  width: 800,
  height: 600,
  url: `/files/u1/${id}.png`,
  ...over,
});
const html = (items: AgentAttachmentDto[], size?: number) =>
  renderToStaticMarkup(<MessageAttachments items={items} size={size} />);

describe("消息气泡上方的附件缩略图", () => {
  test("没有附件：什么都不渲染", () => {
    expect(html([])).toBe("");
  });

  test("每张图一个缩略图按钮，地址用缩略图变体（/files/ 稳定地址 + ?v=thumb）", () => {
    const out = html([att("1"), att("2")]);
    expect(out).toContain('src="/files/u1/1.png?v=thumb"');
    expect(out).toContain('src="/files/u1/2.png?v=thumb"');
    expect(out.indexOf("1.png")).toBeLessThan(out.indexOf("2.png"));
    expect(out).toContain('aria-label="查看大图：图片 1.png"');
  });

  test("不是 /files/ 的地址（外链）原样用，不拼变体", () => {
    expect(html([att("1", { url: "https://cdn.example.com/a.png" })])).toContain(
      'src="https://cdn.example.com/a.png"',
    );
  });

  test("默认 48px，可以缩小（排队插话里用）", () => {
    expect(html([att("1")])).toContain("width:48px");
    expect(html([att("1")], 32)).toContain("width:32px");
  });
});
