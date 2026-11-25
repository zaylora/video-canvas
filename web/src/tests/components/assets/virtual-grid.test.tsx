import { afterAll, beforeAll, expect, test } from "bun:test";

import { VirtualGrid } from "@/components/assets/virtual-grid";
import { installDom, type DomEnv } from "@/tests/helpers/dom";

let dom: DomEnv;

beforeAll(async () => {
  dom = await installDom();
});

afterAll(() => dom.restore());

test("弹层把文档滚动转移到 body 时，网格保留当前可见的卡片", async () => {
  const { win } = dom;
  Object.defineProperty(win.HTMLElement.prototype, "offsetHeight", { get: () => 100 });
  Object.defineProperty(win, "scrollTo", { value: () => {} });
  const items = Array.from({ length: 180 }, (_, i) => `音色 ${i}`);
  const { container, unmount } = await dom.mount(
    <VirtualGrid
      items={items}
      columns={{ minCell: 116, gap: 12, minColumns: 3 }}
      estimateRowHeight={() => 100}
      getKey={(item) => item}
      renderItem={(item) => <button>{item}</button>}
    />,
  );
  const { body, documentElement } = win.document;
  const setWindowY = (value: number) => {
    Object.defineProperty(win, "scrollY", { configurable: true, value });
    win.dispatchEvent(new win.Event("scroll"));
  };
  const visibleCards = () =>
    Array.from(container.querySelectorAll("button"), (button) => button.textContent);

  try {
    await dom.act(async () => setWindowY(2000));
    const before = visibleCards();
    expect(before).toContain("音色 80");
    expect(before).not.toContain("音色 0");

    await dom.act(async () => {
      body.scrollTop = 2000;
      documentElement.setAttribute("data-base-ui-scroll-locked", "");
      setWindowY(0);
    });
    expect(visibleCards()).toEqual(before);

    await dom.act(async () => {
      body.scrollTop = 0;
      documentElement.removeAttribute("data-base-ui-scroll-locked");
      setWindowY(2000);
    });
    expect(visibleCards()).toEqual(before);
  } finally {
    await unmount();
  }
});
