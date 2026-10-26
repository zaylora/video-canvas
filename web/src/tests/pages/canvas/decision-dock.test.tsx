import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { AgentApprovalDto, AgentAskPayload } from "@/api/agent/type";
import type { AgentController } from "@/hooks/use-agent-controller";
import { DecisionDock } from "@/pages/canvas/agent/decision-dock";

const LONG =
  "合并组：立绘＋设定板合成一个组（立绘在上、设定板在下，框住所有节点，把竖图和横图都放进同一个组，之后再统一整理）";

const ask = (options: string[]): AgentApprovalDto => ({
  id: "a1",
  run_id: "r1",
  kind: "ask",
  status: "pending",
  payload: {
    question: "这两类组你想怎么处理？",
    kind: "choice",
    options,
  } satisfies AgentAskPayload,
  quote_credits: 0,
  decision: null,
  expires_at: "2099-01-01T00:00:00Z",
});

const render = (options: string[]) =>
  renderToStaticMarkup(
    <DecisionDock
      approval={ask(options)}
      ctl={{ decide: async () => {} } as unknown as AgentController}
    />,
  );

/** 取出所有选项按钮的 class 属性 */
const optionClasses = (html: string) =>
  [...html.matchAll(/<button[^>]*role="option"[^>]*>/g)].map(
    (m) => /class="([^"]*)"/.exec(m[0])?.[1] ?? "",
  );

describe("决定框：Agent 提问的选项", () => {
  test("长选项完整显示，按钮不再写死 h-9", () => {
    const html = render([LONG, "算了"]);
    expect(html).toContain(LONG);
    const classes = optionClasses(html);
    expect(classes).toHaveLength(2);
    for (const cls of classes) {
      expect(cls.split(/\s+/)).not.toContain("h-9");
      expect(cls.split(/\s+/)).toContain("min-h-9");
    }
  });

  test("选项文字允许换行：不带 truncate，长串可折断", () => {
    const html = render([LONG]);
    const label = new RegExp(`<span class="([^"]*)">${LONG}</span>`).exec(html);
    expect(label).not.toBeNull();
    const cls = label![1].split(/\s+/);
    expect(cls).not.toContain("truncate");
    expect(cls).toContain("wrap-anywhere");
  });

  test("序号圆点与文字顶对齐，多行时不跟着居中", () => {
    const [cls] = optionClasses(render([LONG]));
    expect(cls.split(/\s+/)).toContain("items-start");
    expect(cls.split(/\s+/)).not.toContain("items-center");
  });
});
