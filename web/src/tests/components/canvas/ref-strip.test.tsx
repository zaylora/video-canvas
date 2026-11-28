import { afterAll, beforeAll, describe, expect, mock, spyOn, test } from "bun:test";

import type { RefStrip as RefStripType } from "@/components/canvas/ref-strip";
import { installDom, type DomEnv } from "@/tests/helpers/dom";

/**
 * 引用条：点上游缩略图是把画布视口飞到那个源节点，不再往提示词里插 chip。
 */

// 引用条会拉官方音色：只拦住底层请求（用完还原），不整模块替换。
// bun 的 mock.module 会跨文件生效，换掉 @/api/official-voice 会让别的测试文件拿到假的接口。
const service = (await import("@/utils/requests/service")).default;
const getSpy = spyOn(service, "get").mockResolvedValue({ items: [], total: 0 } as never);

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
afterAll(() => {
  dom.restore();
  getSpy.mockRestore();
});

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

  test("模型支持参考音频时，+ 菜单提供 音色库入口", async () => {
    const { container, unmount } = await dom.mount(
      <RefStrip
        items={[]}
        manual={[]}
        candidates={() => []}
        onLink={() => {}}
        onUnlink={() => {}}
        uploadKinds={[]}
        onUpload={() => {}}
        officialVoice={{ onPick: () => {} }}
        onRemoveManual={() => {}}
        errors={[]}
      />,
    );
    await dom.click(container.querySelector('button[aria-label="引用画布里的素材"]'));
    await dom.flush();
    expect(dom.win.document.body.textContent).toContain("音色库");
    await unmount();
  });

  test("添加参考图打开现有的画布和上传菜单", async () => {
    const { container, unmount } = await dom.mount(
      <RefStrip
        items={[]}
        manual={[]}
        candidates={() => [{ id: "image", kind: "image", label: "场景图" }]}
        onLink={() => {}}
        onUnlink={() => {}}
        uploadKinds={[{ key: "images", kind: "image", label: "参考图片", maxMb: 10 }]}
        onUpload={() => {}}
        onRemoveManual={() => {}}
        errors={[]}
        referencePrompt={{ action: "添加参考图", hint: "可选画布图片，或上传参考图" }}
      />,
    );
    expect(container.textContent).toContain("可选画布图片");
    await dom.click(container.querySelector('button[aria-label="添加参考图"]'));
    await dom.flush();
    expect(dom.win.document.body.textContent).toContain("场景图");
    expect(dom.win.document.body.textContent).toContain("上传参考素材");
    await unmount();
  });
});
