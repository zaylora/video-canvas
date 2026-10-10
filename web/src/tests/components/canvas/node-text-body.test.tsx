import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { NodeTextBody } from "@/components/canvas/node-body";

const render = (props: Partial<Parameters<typeof NodeTextBody>[0]>) =>
  renderToStaticMarkup(
    <NodeTextBody
      status="idle"
      icon={null}
      placeholder="双击输入文字，或选中后写提示词生成"
      {...props}
    />,
  );

describe("文本节点正文", () => {
  test("没有内容时摆占位说明", () => {
    expect(render({})).toContain("双击输入文字，或选中后写提示词生成");
  });

  test("有内容时摊开正文", () => {
    const out = render({ status: "done", text: "粘贴来的一段话" });
    expect(out).toContain("粘贴来的一段话");
  });

  test("只读的正文不挡拖拽：抓着正文就能拖动节点", () => {
    const out = render({ status: "done", text: "粘贴来的一段话" });
    expect(out).not.toContain("nodrag");
  });

  test("给了 onTextChange 就提示可以双击编辑，空节点、有内容、失败都一样", () => {
    const onTextChange = () => {};
    expect(render({ onTextChange })).toContain('title="双击编辑"');
    expect(render({ status: "done", text: "x", onTextChange })).toContain('title="双击编辑"');
    expect(render({ status: "error", error: "失败了", onTextChange })).toContain(
      'title="双击编辑"',
    );
  });

  test("没给 onTextChange 不能编辑", () => {
    expect(render({ status: "done", text: "x" })).not.toContain("双击编辑");
  });

  test("生成中锁定，即使给了 onTextChange 也不能编辑", () => {
    const out = render({ status: "running", onTextChange: () => {} });
    expect(out).not.toContain("双击编辑");
    expect(out).toContain("生成中");
  });
});
