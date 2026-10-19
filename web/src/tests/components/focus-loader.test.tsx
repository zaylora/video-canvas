import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { FocusLoader } from "@/components/focus-loader";

const render = (progress?: number) =>
  renderToStaticMarkup(
    <FocusLoader
      icon={<i />}
      title="导演台"
      subtitle="正在加载 3D 引擎…"
      label="正在打开导演台"
      progress={progress}
      ready={false}
      onOpen={() => {}}
      onDone={() => {}}
    />,
  );

describe("FocusLoader 进度条", () => {
  test("不给进度时不画进度条（进画布、进后台照旧）", () => {
    const out = render();
    expect(out).not.toContain("progressbar");
    expect(out).not.toContain("%");
  });

  test("给了进度：画进度条和百分比，并标上读屏用的数值", () => {
    const out = render(0.37);
    expect(out).toContain('role="progressbar"');
    expect(out).toContain('aria-valuenow="37"');
    expect(out).toContain("37%");
    expect(out).toContain("scaleX(0.37)");
  });

  test("进度越界会被夹在 0 到 100% 之间", () => {
    expect(render(1.5)).toContain('aria-valuenow="100"');
    expect(render(-1)).toContain('aria-valuenow="0"');
  });

  test("标题和说明照常显示", () => {
    const out = render(0.5);
    expect(out).toContain("导演台");
    expect(out).toContain("正在加载 3D 引擎…");
  });
});

describe("FocusLoader 图标", () => {
  const renderIcon = (plainIcon?: boolean) =>
    renderToStaticMarkup(
      <FocusLoader
        icon={<i data-icon />}
        plainIcon={plainIcon}
        title="连镜"
        subtitle="正在打开画布"
        label="正在打开画布"
        ready={false}
        onOpen={() => {}}
        onDone={() => {}}
      />,
    );

  test("默认把图标装进深色圆角块，并做聚焦显影", () => {
    const out = renderIcon();
    expect(out).toContain("rounded-[22px]");
    expect(out).toMatch(/canvas-loader-focus[^"]*bg-foreground/);
  });

  test("plainIcon：图标自带动画，不套色块也不做聚焦显影", () => {
    const out = renderIcon(true);
    expect(out).toContain("data-icon");
    expect(out).not.toContain("rounded-[22px]");
    expect(out).not.toMatch(/canvas-loader-focus[^"]*bg-foreground/);
  });
});
