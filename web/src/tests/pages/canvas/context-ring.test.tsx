import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { TooltipProvider } from "@/components/ui/tooltip";
import { ContextRing } from "@/pages/canvas/agent/context-ring";

const html = (tokens: number, window: number) =>
  renderToStaticMarkup(
    <TooltipProvider>
      <ContextRing tokens={tokens} window={window} />
    </TooltipProvider>,
  );

describe("上下文环", () => {
  test("按钮说明里是上下文用量，不出现「预算」", () => {
    const out = html(50_000, 200_000);
    expect(out).toContain('aria-label="上下文用量');
    expect(out).not.toContain("预算");
  });

  test("按钮说明带上已用和上限", () => {
    expect(html(50_000, 200_000)).toContain("50K / 200K");
  });

  test("没配窗口时只说已用，不显示上限", () => {
    const out = html(5000, 0);
    expect(out).toContain("5K");
    expect(out).not.toContain("/ 0");
  });

  test("用量越高环的颜色越警示：接近上限变黄，到顶变红", () => {
    expect(html(50_000, 200_000)).toContain("stroke-muted-foreground");
    expect(html(150_000, 200_000)).toContain("stroke-status-warning");
    expect(html(190_000, 200_000)).toContain("stroke-destructive");
  });
});
