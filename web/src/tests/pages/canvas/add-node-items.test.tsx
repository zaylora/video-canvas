import { describe, expect, test } from "bun:test";

import { ASSET_LIBRARY_ACTION, UPLOAD_ACTION } from "@/constants/canvas";
import { buildAddNodeItems } from "@/pages/canvas/chrome/add-node-items";

describe("buildAddNodeItems：添加节点菜单的条目", () => {
  test("顺序：图片、视频、文本、音频，上传素材隔一条线放最后", () => {
    const items = buildAddNodeItems();
    expect(items.map((item) => item.value)).toEqual([
      "image",
      "video",
      "script",
      "audio",
      UPLOAD_ACTION,
      ASSET_LIBRARY_ACTION,
    ]);
    // 「从资产库添加」紧挨着上传素材，同属一组，不另起分隔线
    expect(items.map((item) => !!item.separated)).toEqual([
      false,
      false,
      false,
      false,
      true,
      false,
    ]);
  });

  test("名字沿用「图片生成 / 视频生成 / 文本 / 音频 / 上传素材」", () => {
    expect(buildAddNodeItems().map((item) => item.label)).toEqual([
      "图片生成",
      "视频生成",
      "文本",
      "音频",
      "上传素材",
      "从资产库添加",
    ]);
  });

  test("每项只有一行文字：不带说明，也不带行尾提示", () => {
    for (const item of buildAddNodeItems()) {
      expect(Object.keys(item)).not.toContain("description");
      expect(Object.keys(item)).not.toContain("hint");
    }
  });

  test("接不上的种类被禁用；三种素材都接不上，上传素材才禁用", () => {
    const onlyVideo = buildAddNodeItems((kind) => kind !== "video");
    expect(onlyVideo.map((item) => !!item.disabled)).toEqual([
      true,
      false,
      true,
      true,
      false,
      false,
    ]);

    const none = buildAddNodeItems(() => true);
    expect(none.find((item) => item.value === UPLOAD_ACTION)?.disabled).toBe(true);
  });

  test("从资产库添加不受拉线类型限制：只建节点不连线，始终可点", () => {
    const none = buildAddNodeItems(() => true);
    expect(none.at(-1)?.value).toBe(ASSET_LIBRARY_ACTION);
    expect(none.at(-1)?.disabled).toBeFalsy();
  });
});
