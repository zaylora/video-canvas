import { afterAll, beforeAll, describe, expect, mock, test } from "bun:test";

import type { RefStrip as RefStripType } from "@/components/canvas/ref-strip";
import { installDom, type DomEnv } from "@/tests/helpers/dom";

/**
 * 引用条：点上游缩略图是把画布视口飞到那个源节点，不再往提示词里插 chip。
 */

const located: string[] = [];
const focusNodeModule = await import("@/components/canvas-editor/use-focus-node");
mock.module("@/components/canvas-editor/use-focus-node", () => ({
  ...focusNodeModule,
  useLocateNode: () => (id: string) => {
    located.push(id);
    return true;
  },
}));

let dom: DomEnv;
let RefStrip: typeof RefStripType;
beforeAll(async () => {
  dom = await installDom();
  RefStrip = (await import("@/components/canvas/ref-strip")).RefStrip;
});
afterAll(() => dom.restore());

describe("引用条上的上游缩略图", () => {
  test("点击定位到画布上的源节点，不插入到提示词", async () => {
    const { container, unmount } = await dom.mount(
      <RefStrip
        items={[
          { id: "n1", kind: "image", label: "图片", src: "https://example.com/a.png", used: true },
        ]}
        manual={[]}
        candidates={() => []}
        onLink={() => {}}
        onUnlink={() => {}}
        uploadKinds={[]}
        onUpload={() => {}}
        onRemoveManual={() => {}}
        errors={[]}
      />,
    );
    const thumb = container.querySelector('button[aria-label^="引用：图片"]');
    expect(thumb?.getAttribute("aria-label")).not.toContain("插入到提示词");
    await dom.click(thumb);
    expect(located).toEqual(["n1"]);
    await unmount();
  });
});
