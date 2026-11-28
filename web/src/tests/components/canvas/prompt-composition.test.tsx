import { afterAll, beforeAll, expect, test } from "bun:test";
import { useEffect, useState, type ComponentType } from "react";
import type { Editor } from "@tiptap/core";

import { installDom, type DomEnv } from "@/tests/helpers/dom";
import type { PromptEditor as PromptEditorComponent } from "@/components/canvas/prompt-mention";

let dom: DomEnv;
let PromptEditor: ComponentType<React.ComponentProps<typeof PromptEditorComponent>>;

beforeAll(async () => {
  dom = await installDom();
  ({ PromptEditor } = await import("@/components/canvas/prompt-mention"));
});

afterAll(() => dom.restore());

test.each([
  ["确认后才回传", "共同起点 中", ["共同起点 中"], "共同起点 中"],
  ["取消后应用远端值", "共同起点", [], "远端已提交"],
] as const)("提示词组字 %s", async (_label, confirmed, expected, visible) => {
  const emitted: string[] = [];
  let receive = (_value: string) => {};
  function Harness() {
    const [value, setValue] = useState("共同起点");
    useEffect(() => {
      receive = setValue;
    }, []);
    return (
      <PromptEditor
        value={value}
        onValueChange={(next) => {
          emitted.push(next);
          setValue(next);
        }}
        onSubmit={() => {}}
        placeholder="提示词"
      />
    );
  }
  const mounted = await dom.mount(<Harness />);
  try {
    const field = mounted.container.querySelector('[role="textbox"]') as HTMLElement & {
      editor: Editor;
    };
    await dom.act(async () => {
      field.dispatchEvent(new dom.win.CompositionEvent("compositionstart", { bubbles: true }));
      field.editor.commands.setContent("共同起点 zhong");
    });
    expect(emitted).toEqual([]);
    await dom.act(async () => receive("远端已提交"));
    expect(field.textContent).toBe("共同起点 zhong");
    await dom.act(async () => {
      field.editor.commands.setContent(confirmed);
      field.dispatchEvent(new dom.win.CompositionEvent("compositionend", { bubbles: true }));
    });
    await dom.flush(30);
    expect(emitted).toEqual(expected);
    expect(field.textContent).toBe(visible);
  } finally {
    await mounted.unmount();
    await dom.flush();
  }
});
