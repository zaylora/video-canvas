import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { Library } from "@/pages/drama/studio/library";
import { StudioContext, type Studio } from "@/pages/drama/studio/use-studio";

const asset = {
  id: "scene-1",
  type: "scene" as const,
  name: "卧室",
  look: "",
  label: "卧室",
  episodes: [1],
  kind: "image" as const,
  src: null,
  generating: false,
};

describe("片场资产库", () => {
  test("资产只能拖入提示词，单击或键盘激活不会将其添加到右侧", () => {
    const html = renderToStaticMarkup(
      <StudioContext.Provider value={{ assets: [asset], assetsStatus: "ready", no: 1 } as Studio}>
        <Library />
      </StudioContext.Provider>,
    );
    const row = html.match(/<[^>]+draggable="true"[^>]*>/)?.[0];
    expect(row).toBeDefined();
    expect(row).toContain('title="拖到提示词里"');
    expect(row).not.toMatch(/^<button\b/);
    expect(row).not.toContain("tabindex");
    expect(html).toContain("卧室");
  });
});
