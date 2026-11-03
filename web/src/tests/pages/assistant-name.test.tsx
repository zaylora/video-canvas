import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { ASSISTANT_NAME } from "@/constants/agent";
import { AssistantTrigger } from "@/pages/admin/assistant/assistant-trigger";
import { AgentLauncher } from "@/pages/canvas/chrome/agent-launcher";

/**
 * 画布和后台是同一个助手「小镜」：入口按钮、面板的无障碍名称都叫小镜，
 * 不再分别叫「Agent」「画布 Agent」「后台助手」「助手」
 */
describe("助手统一叫小镜", () => {
  test("名字常量", () => {
    expect(ASSISTANT_NAME).toBe("小镜");
  });

  test("画布右下角入口显示小镜", () => {
    const html = renderToStaticMarkup(
      <AgentLauncher open={false} available running={false} onToggle={() => {}} />,
    );
    expect(html).toContain(">小镜<");
    expect(html).toContain('aria-label="小镜"');
    expect(html).not.toContain(">Agent<");
  });

  test("后台顶栏入口显示小镜", () => {
    const html = renderToStaticMarkup(<AssistantTrigger />);
    expect(html).toContain(">小镜<");
    expect(html).toContain('aria-label="小镜"');
  });
});
