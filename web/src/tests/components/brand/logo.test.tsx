import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { Logo } from "@/components/brand/logo";

const count = (html: string, needle: string) => html.split(needle).length - 1;

describe("Logo「流光」", () => {
  test("两道弧加一缕光，弧跟随文字色，光用品牌 token", () => {
    const out = renderToStaticMarkup(<Logo size={32} />);
    expect(out).toContain('data-slot="logo"');
    expect(count(out, "<path")).toBe(3);
    expect(count(out, 'fill="currentColor"')).toBe(2);
    expect(out).toContain("var(--beam)");
    expect(out).not.toContain("<rect");
  });

  test("不超过 24px 换小尺寸版，光束用实色", () => {
    const small = renderToStaticMarkup(<Logo size={24} />);
    const big = renderToStaticMarkup(<Logo size={25} />);
    expect(small).toContain('data-variant="small"');
    expect(small).not.toContain("linearGradient");
    expect(big).toContain('data-variant="large"');
    expect(big).toContain("linearGradient");
  });

  test("bright 用永远偏亮的光", () => {
    expect(renderToStaticMarkup(<Logo bright />)).toContain("var(--beam-bright)");
  });

  test("animated 拆成光晕、左弧、右弧、光束四层，供加载动画分别驱动", () => {
    const out = renderToStaticMarkup(<Logo size={44} animated />);
    for (const part of ["glow", "left", "right", "beam"]) {
      expect(out).toContain(`data-part="${part}"`);
    }
    expect(out).toContain('data-slot="logo"');
    expect(out).toContain("logo-anim");
  });
});
