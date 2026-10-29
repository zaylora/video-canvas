import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { IdeaDock } from "@/pages/login/idea-dock";
import { WordSwapTitle } from "@/pages/login/word-swap-title";

const noop = () => {};
const dock = (open: boolean, idea = "") =>
  renderToStaticMarkup(
    <IdeaDock
      open={open}
      idea={idea}
      title="登录"
      onIdeaChange={noop}
      onStart={noop}
      onClose={noop}
    >
      <form data-testid="auth-form" />
    </IdeaDock>,
  );

describe("登录页：胶囊 ⇄ 卡片", () => {
  test("收着时是一颗胶囊：写想法的输入框 + 「开始创作」，没有对话框", () => {
    const out = dock(false);
    expect(out).toContain('data-slot="idea-dock"');
    expect(out).toContain("写下脑海里的画面…");
    expect(out).toContain("开始创作");
    expect(out).toMatch(/role="dialog"[^>]*aria-hidden="true"/);
    expect(out).toContain('data-open="false"');
  });

  test("展开后是登录卡片：标题、关闭按钮和表单都在，胶囊那层不可交互", () => {
    const out = dock(true);
    expect(out).toContain('data-open="true"');
    expect(out).toMatch(/role="dialog"(?![^>]*aria-hidden)/);
    expect(out).toContain("登录");
    expect(out).toContain('aria-label="关闭（Esc）"');
    expect(out).toContain('data-testid="auth-form"');
    expect(out).toMatch(/data-slot="idea-capsule"[^>]*inert/);
  });

  test("写了想法时，卡片顶部提示登录后继续", () => {
    expect(dock(true, "雨夜街头，女主回眸")).toContain("登录后继续：雨夜街头，女主回眸");
    expect(dock(true, "")).not.toContain("登录后继续");
  });
});

describe("登录页标题：词轮换", () => {
  test("读屏读整句，不读轮换中的碎片", () => {
    const out = renderToStaticMarkup(<WordSwapTitle />);
    expect(out).toContain('aria-label="从一句话，到一部片。"');
    expect(out).toContain("片。");
    expect(out).toContain("电影。");
    expect(out).toContain("短剧。");
  });
});
