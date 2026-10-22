import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { NodeTextBody } from "@/components/canvas/node-body";

const render = (props: Partial<Parameters<typeof NodeTextBody>[0]>) =>
  renderToStaticMarkup(
    <NodeTextBody status="idle" icon={null} placeholder="双击输入文字" {...props} />,
  );

describe("文本节点正文", () => {
  test("没有内容时摆占位说明", () => {
    expect(render({})).toContain("双击输入文字");
  });

  test("有内容时摊开正文", () => {
    const out = render({ status: "done", text: "粘贴来的一段话" });
    expect(out).toContain("粘贴来的一段话");
  });

  test("有正文就显示，不看状态：撤销 Agent 改动可能只还原了状态，正文不能因此藏起来", () => {
    expect(render({ status: "idle", text: "还在的正文" })).toContain("还在的正文");
  });

  test("有内容时和占位态同一副 16:9 画幅，生成完节点不变矮", () => {
    const filled = render({ status: "done", text: "一句话" });
    expect(filled).toContain("aspect-ratio:1.7777777777777777");
    expect(filled).not.toContain("max-h-");
  });

  test("只读的正文不挡拖拽：抓着正文就能拖动节点", () => {
    const out = render({ status: "done", text: "粘贴来的一段话" });
    expect(out).not.toContain("nodrag");
  });

  test("给了 onEdit 就提示可以双击编辑，空节点、有内容、失败都一样", () => {
    const onEdit = () => {};
    expect(render({ onEdit })).toContain('title="双击编辑"');
    expect(render({ status: "done", text: "x", onEdit })).toContain('title="双击编辑"');
    expect(render({ status: "error", error: "失败了", onEdit })).toContain('title="双击编辑"');
  });

  test("没给 onEdit 不能编辑", () => {
    expect(render({ status: "done", text: "x" })).not.toContain("双击编辑");
  });

  test("生成中锁定，即使给了 onEdit 也不能编辑", () => {
    const out = render({ status: "running", onEdit: () => {} });
    expect(out).not.toContain("双击编辑");
    expect(out).toContain("生成中");
  });

  test("正文按 Markdown 解析：标题、加粗、分隔线渲染成元素，不露出源码符号", () => {
    const out = render({ status: "done", text: "## 第一集\n\n**类型：** 都市\n\n---" });
    expect(out).toContain("<h3");
    expect(out).toContain("<strong");
    expect(out).toContain("<hr");
    expect(out).not.toContain("##");
    expect(out).not.toContain("**");
  });

  test("单个换行也换行：剧本里一行一项不会被合成一段", () => {
    const out = render({ status: "done", text: "**类型：** 都市\n**风格：** 写实" });
    expect(out).toContain("<br");
  });

  test("不再有原地编辑框：双击只通知外层打开编辑弹窗", () => {
    const out = render({ status: "done", text: "x", onEdit: () => {} });
    expect(out).not.toContain("<textarea");
  });
});
