import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { Disclosure, DisclosureContent, DisclosureTrigger } from "@/components/admin-ui/disclosure";

const render = (defaultOpen?: boolean) =>
  renderToStaticMarkup(
    <Disclosure defaultOpen={defaultOpen}>
      <DisclosureTrigger>标题行</DisclosureTrigger>
      <DisclosureContent>
        <p>折叠里的内容</p>
      </DisclosureContent>
    </Disclosure>,
  );

describe("折叠区", () => {
  test("默认折叠：只有触发行，aria-expanded 为 false，内容不渲染", () => {
    const out = render();
    expect(out).toContain("标题行");
    expect(out).toContain('aria-expanded="false"');
    expect(out).not.toContain("折叠里的内容");
  });

  test("defaultOpen：内容渲染，aria-expanded 为 true，触发按钮指向内容区", () => {
    const out = render(true);
    expect(out).toContain('aria-expanded="true"');
    expect(out).toContain("折叠里的内容");
    const controls = /aria-controls="([^"]+)"/.exec(out)?.[1];
    expect(controls).toBeTruthy();
    expect(out).toContain(`id="${controls}"`);
  });

  test("触发行是 button，不会提交表单", () => {
    expect(render()).toMatch(/<button[^>]*type="button"/);
  });
});
