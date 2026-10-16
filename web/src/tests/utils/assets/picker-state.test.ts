import { describe, expect, test } from "bun:test";

import type { AssetDto } from "@/api/asset/type";
import {
  INITIAL_PICKER_VIEW,
  applyUploadDone,
  enterFolder,
  leaveFolder,
  switchKind,
  switchSource,
  uploadFolder,
  type PickerView,
} from "@/utils/assets/picker-state";

const asset = (id: number, kind: AssetDto["kind"] = "image"): AssetDto => ({
  id: String(id),
  kind,
  url: `/files/${id}`,
  mimeType: "x/y",
  byteSize: 1,
  width: null,
  height: null,
  durationMs: null,
  fileName: `${id}`,
});

describe("弹框视图状态", () => {
  test("初始：个人资产库、图片、根目录", () => {
    expect(INITIAL_PICKER_VIEW).toEqual({ source: "library", kind: "image", folder: null });
  });

  test("切来源：保留类型标签，回到根目录", () => {
    const view: PickerView = { source: "library", kind: "video", folder: "f1" };
    expect(switchSource(view, "history")).toEqual({
      source: "history",
      kind: "video",
      folder: null,
    });
  });

  test("切类型：保留来源和文件夹", () => {
    const view: PickerView = { source: "library", kind: "image", folder: "f1" };
    expect(switchKind(view, "audio")).toEqual({ source: "library", kind: "audio", folder: "f1" });
  });

  test("进入、离开文件夹；生成历史没有文件夹", () => {
    const view = enterFolder(INITIAL_PICKER_VIEW, "f1");
    expect(view.folder).toBe("f1");
    expect(leaveFolder(view).folder).toBeNull();
    const history = switchSource(INITIAL_PICKER_VIEW, "history");
    expect(enterFolder(history, "f1")).toBe(history);
  });
});

describe("uploadFolder：弹框内上传归入哪个文件夹", () => {
  test("个人资产库里归入当前文件夹，根目录是 null", () => {
    expect(uploadFolder({ source: "library", kind: "image", folder: "f1" })).toBe("f1");
    expect(uploadFolder(INITIAL_PICKER_VIEW)).toBeNull();
  });
});

describe("applyUploadDone：上传完成后自动勾选", () => {
  test("勾上新素材，不论什么类型", () => {
    const result = applyUploadDone([asset(1)], asset(2, "video"));
    expect(result.selected.map((a) => a.id)).toEqual(["1", "2"]);
    expect(result.dropped).toBe(0);
  });

  test("已选满 20 个：不勾选，报丢弃数", () => {
    const full = Array.from({ length: 20 }, (_, i) => asset(i + 1));
    const result = applyUploadDone(full, asset(99, "audio"));
    expect(result.selected).toHaveLength(20);
    expect(result.dropped).toBe(1);
  });
});
