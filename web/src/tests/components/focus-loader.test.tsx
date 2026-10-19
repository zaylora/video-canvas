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
