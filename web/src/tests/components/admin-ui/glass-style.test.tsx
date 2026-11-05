import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { renderToStaticMarkup } from "react-dom/server";

import { DataPanel } from "@/components/admin-ui/data-panel";
import { ListPanel } from "@/components/admin-ui/list-panel";
import { Segmented } from "@/components/admin-ui/segmented";
import { StatCard } from "@/components/admin-ui/stat-card";

/** 清透面板的关键几项：半透明底、背景模糊、大圆角 */
const expectGlass = (html: string) => {
  expect(html).toContain("bg-chrome");
  expect(html).toContain("backdrop-blur-xl");
  expect(html).toContain("rounded-3xl");
};

describe("后台面板统一用清透外观", () => {
  test("表格卡片、主从列表、指标卡都是清透面板", () => {
    expectGlass(renderToStaticMarkup(<DataPanel />));
    expectGlass(renderToStaticMarkup(<ListPanel />));
    expectGlass(renderToStaticMarkup(<StatCard />));
  });

  test("分段选择是清透胶囊", () => {
    const html = renderToStaticMarkup(<Segmented />);
    expect(html).toContain("bg-chrome");
    expect(html).toContain("rounded-full");
  });
});

describe("后台配色跟前台一致", () => {
  const css = readFileSync(join(import.meta.dir, "../../../index.css"), "utf8");

  test("浅色下后台和首页外壳共用同一组米色变量", () => {
    expect(css).toMatch(/:root:not\(\.dark\) \.home-theme,\s*:root\.admin-theme:not\(\.dark\) \{/);
  });

  test("后台不再自己覆盖底色、卡片、描边这些中性色", () => {
    const admin = css.match(/\n\.admin-theme \{([^}]*)\}/)?.[1] ?? "";
    const dark = css.match(/\n\.dark\.admin-theme \{([^}]*)\}/)?.[1] ?? "";
    for (const block of [admin, dark]) {
      expect(block).not.toMatch(/--(background|card|border|muted|sidebar):/);
    }
  });
});
