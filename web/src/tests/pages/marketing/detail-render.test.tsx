import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { MemoryRouter } from "react-router";

import { project } from "./fixtures";
import { MarketingComposer } from "@/components/marketing/marketing-composer";
import { ProjectCard } from "@/pages/marketing/project-grid";

describe("营销生成框与列表卡渲染", () => {
  test("列表页生成框：三个上传块、平台、角度、模型、参数、写脚本键", () => {
    const out = renderToStaticMarkup(
      <MemoryRouter>
        <MarketingComposer />
      </MemoryRouter>,
    );
    expect(out).toContain("商品");
    expect(out).toContain("模特");
    expect(out).toContain("参考");
    expect(out).toContain("抖音");
    expect(out).toContain("自动 ×2");
    expect(out).toContain("9:16 · 720P · 15s · 中");
    expect(out).toContain('aria-label="写脚本"');
    // 投放平台用品牌图标：抖音是 TikTok 的音符标（simple-icons），不是线性的音符图标
    expect(out).toContain('d="M12.525.02c1.31');
    expect(out).not.toContain("lucide-music-2");
    expect(out).toContain("商品名、卖点、想要的感觉，也可以不写");
  });

  test("详情页 dock：商品图固定、占位文案不同", () => {
    const out = renderToStaticMarkup(
      <MemoryRouter>
        <MarketingComposer project={project} />
      </MemoryRouter>,
    );
    expect(out).toContain("换个说法、补充卖点，再写一轮");
    expect(out).toContain('data-placement="dock"');
  });

  test("列表卡：标题、状态行、状态条", () => {
    const out = renderToStaticMarkup(
      <MemoryRouter>
        <ProjectCard
          project={{
            id: "a",
            title: "便携榨汁杯",
            coverUrl: "",
            scripting: false,
            rendering: 1,
            lastRoundFailed: false,
            scriptCounts: { draft: 1, rendering: 1, done: 2, failed: 0 },
            bars: ["done", "rendering", "draft"],
            updatedAt: "2026-11-10T10:00:00Z",
          }}
          index={0}
          deleting={false}
          onDelete={() => undefined}
        />
      </MemoryRouter>,
    );
    expect(out).toContain("便携榨汁杯");
    expect(out).toContain("出片中");
    expect(out.split("h-[3px]").length - 1).toBe(3);
    expect(out).toContain('href="/marketing/a"');
  });
});
