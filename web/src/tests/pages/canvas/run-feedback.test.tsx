import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { FeedbackBar, type FeedbackBarProps } from "@/pages/canvas/agent/run-feedback";

const noop = () => undefined;
const base: FeedbackBarProps = {
  feedback: undefined,
  saving: false,
  formOpen: false,
  draft: { reasons: [], comment: "" },
  onThumb: noop,
  onToggleReason: noop,
  onComment: noop,
  onCancelForm: noop,
  onSubmitForm: noop,
};
const html = (props: Partial<FeedbackBarProps> = {}) =>
  renderToStaticMarkup(<FeedbackBar {...base} {...props} />);

/** 取出某个评价按钮的开标签 */
const thumb = (out: string, rating: 1 | -1) =>
  out.match(new RegExp(`<button[^>]*data-rating="${rating}"[^>]*>`))?.[0] ?? "";

/** 开标签上有 disabled 属性（class 里的 disabled: 前缀不算） */
const isDisabled = (tag: string) => / disabled=""/.test(tag);

describe("反馈条：按钮状态", () => {
  test("默认：按钮可点，没有任何「运行结束后可评价」之类的提示（运行中根本不渲染评价条）", () => {
    const out = html();
    expect(isDisabled(thumb(out, 1))).toBe(false);
    expect(isDisabled(thumb(out, -1))).toBe(false);
    expect(out).not.toContain("运行结束后可评价");
    expect(thumb(out, 1)).toContain('aria-pressed="false"');
  });

  test("已点赞：👍 按下，小字感谢反馈", () => {
    const out = html({ feedback: { rating: 1, reasons: [], comment: "" } });
    expect(thumb(out, 1)).toContain('aria-pressed="true"');
    expect(thumb(out, -1)).toContain('aria-pressed="false"');
    expect(out).toContain("感谢反馈");
  });

  test("已点踩：👎 按下，小字列出原因；没选原因写未选原因", () => {
    const withReasons = html({
      feedback: { rating: -1, reasons: ["wrong_edit", "slow"], comment: "" },
    });
    expect(thumb(withReasons, -1)).toContain('aria-pressed="true"');
    expect(withReasons).toContain("已记录：画布改错了、步骤太多或太慢");
    expect(html({ feedback: { rating: -1, reasons: [], comment: "" } })).toContain(
      "已记录：未选原因",
    );
  });

  test("保存中：两个按钮禁用", () => {
    const out = html({ saving: true });
    expect(isDisabled(thumb(out, 1))).toBe(true);
    expect(isDisabled(thumb(out, -1))).toBe(true);
  });
});

describe("反馈条：点踩表单", () => {
  const open = (props: Partial<FeedbackBarProps> = {}) => html({ formOpen: true, ...props });

  test("没展开：没有原因标签和文本框", () => {
    const out = html();
    expect(out).not.toContain("没理解我的需求");
    expect(out).not.toContain("<textarea");
  });

  test("展开：五个原因标签、限 500 字的文本框、取消与提交", () => {
    const out = open();
    for (const label of ["没理解我的需求", "画布改错了", "步骤太多或太慢", "生成结果不好", "其他"])
      expect(out).toContain(label);
    expect(out).toContain('maxLength="500"');
    expect(out).toContain("取消");
    expect(out).toContain("提交");
    expect(out).toContain("原因可以不选");
  });

  test("已选的原因标签按下；草稿文字回显", () => {
    const out = open({ draft: { reasons: ["misread"], comment: "没看懂" } });
    expect(out).toMatch(/aria-pressed="true"[^>]*>没理解我的需求/);
    expect(out).toMatch(/aria-pressed="false"[^>]*>画布改错了/);
    expect(out).toContain("没看懂");
  });

  test("保存中：提交按钮显示提交中并禁用，原因和文本框也禁用", () => {
    const out = open({ saving: true });
    expect(out).toContain("提交中…");
    expect(isDisabled(out.match(/<textarea[^>]*>/)?.[0] ?? "")).toBe(true);
    expect(isDisabled(out.match(/<button[^>]*>(?:<svg.*?<\/svg>)?提交中…/)?.[0] ?? "")).toBe(true);
  });

  test("展开时不重复显示评价小字（表单就是反馈入口）", () => {
    const out = open({ feedback: { rating: -1, reasons: ["other"], comment: "" } });
    expect(out).not.toContain('data-slot="run-feedback-note"');
  });

  test("位置（设计稿 6.12）：和改动胶囊同一行，胶囊靠左、复制和评价靠右；评价小字在按钮左边", () => {
    const out = html({
      feedback: { rating: 1, reasons: [], comment: "" },
      leading: <span data-test="pill">画布改动</span>,
      copyText: "回答",
    });
    expect(out.indexOf('data-test="pill"')).toBeLessThan(out.indexOf('aria-label="复制"'));
    expect(out.indexOf("感谢反馈")).toBeLessThan(out.indexOf('data-rating="1"'));
    expect(out.indexOf('aria-label="复制"')).toBeLessThan(out.indexOf('data-rating="1"'));
  });

  test("没有回答文字时不显示复制", () => {
    expect(html()).not.toContain('aria-label="复制"');
  });

  test("历史轮：按钮组悬停才出现；已评价的常显", () => {
    expect(html({ quiet: true })).toContain("opacity-0");
    expect(html({ quiet: true, feedback: { rating: 1, reasons: [], comment: "" } })).not.toContain(
      "opacity-0",
    );
    expect(html()).not.toContain("opacity-0");
  });
});
