import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { ListSkeleton } from "@/components/home/sidebar-list";

const html = (rows?: number) => renderToStaticMarkup(<ListSkeleton rows={rows} />);

/** 统计某个 data-slot 出现几次 */
const count = (out: string, slot: string) => out.split(`data-slot="${slot}"`).length - 1;

describe("侧栏列表骨架屏", () => {
  test("默认 4 行，每行一个缩略图块和一条标题条", () => {
    const out = html();
    expect(count(out, "list-skeleton-row")).toBe(4);
    expect(count(out, "skeleton")).toBe(8);
  });

  test("行数可指定", () => {
    expect(count(html(2), "list-skeleton-row")).toBe(2);
  });

  test("行高和缩略图尺寸与真实列表项一致，切换时不跳动", () => {
    const out = html(1);
    expect(out).toContain("h-9");
    expect(out).toContain("size-6");
    expect(out).toContain("rounded-[7px]");
  });

  test("标题条宽度错开，不是整齐的一排", () => {
    const widths = new Set(html(4).match(/width:\s*\d+%/g));
    expect(widths.size).toBeGreaterThan(1);
  });

  test("标记为加载中，并在减少动态效果时停掉脉动", () => {
    const out = html();
    expect(out).toContain("aria-busy");
    expect(out).toContain("motion-reduce:animate-none");
  });
});
