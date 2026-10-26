import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { SteerTray, visibleSteers } from "@/pages/canvas/agent/steer-tray";

const items = (n: number) =>
  Array.from({ length: n }, (_, i) => ({ key: `s${i}`, text: `第 ${i + 1} 条` }));

describe("插话队列折叠", () => {
  test("不超过 3 条全部显示，没有折叠项", () => {
    expect(visibleSteers(items(3), false)).toMatchObject({ shown: items(3), hidden: 0 });
  });

  test("超过 3 条默认只显示前 3 条，其余算进「还有 N 条」", () => {
    const v = visibleSteers(items(5), false);
    expect(v.shown.map((i) => i.key)).toEqual(["s0", "s1", "s2"]);
    expect(v.hidden).toBe(2);
  });

  test("展开后显示全部", () => {
    const v = visibleSteers(items(5), true);
    expect(v.shown).toHaveLength(5);
    expect(v.hidden).toBe(0);
  });
});

describe("插话队列", () => {
  test("没有排队的插话时什么都不渲染", () => {
    expect(renderToStaticMarkup(<SteerTray items={[]} />)).toBe("");
  });

  test("每条一行，只有消息原文，不再带「会读取」之类的说明", () => {
    const out = renderToStaticMarkup(<SteerTray items={items(2)} />);
    expect(out).toContain("第 1 条");
    expect(out).toContain("第 2 条");
    expect(out).not.toContain("会读取");
    expect(out).not.toContain("排队中");
  });

  test("超过 3 条时出现「还有 N 条」按钮", () => {
    const out = renderToStaticMarkup(<SteerTray items={items(5)} />);
    expect(out).toContain("还有 2 条");
    expect(out).not.toContain("第 4 条");
  });

  test("消息原文里的节点引用显示成名字，不露出 @[名字](类型:id)", () => {
    const out = renderToStaticMarkup(
      <SteerTray items={[{ key: "a", text: "把 @[主角](node:n1) 换成夜景" }]} />,
    );
    expect(out).toContain("主角");
    expect(out).not.toContain("node:n1");
  });
});
