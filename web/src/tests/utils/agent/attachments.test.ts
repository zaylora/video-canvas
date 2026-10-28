import { describe, expect, test } from "bun:test";

import {
  attachmentBlockReason,
  clipToLimit,
  toAttachmentsPayload,
  type TrayItem,
} from "@/utils/agent/attachments";
import { MAX_ATTACH_IMAGES } from "@/utils/agent/image-files";

const item = (over: Partial<TrayItem> = {}): TrayItem => ({
  id: "t1",
  name: "图片 1",
  previewUrl: "blob:x",
  status: "done",
  progress: 100,
  asset: { id: "101", url: "/files/a.png", width: 800, height: 600, mimeType: "image/png" },
  ...over,
});

describe("附件托盘：什么时候不能发送", () => {
  test("托盘空着或图片都已上传：不拦", () => {
    expect(attachmentBlockReason([], true)).toBeNull();
    expect(attachmentBlockReason([item(), item({ id: "t2" })], true)).toBeNull();
  });

  test("有图片还在上传：拦，提示上传中", () => {
    expect(
      attachmentBlockReason([item({ status: "uploading", progress: 30, asset: undefined })], true),
    ).toBe("图片上传中…");
  });

  test("有图片上传失败：拦，提示重试或移除", () => {
    expect(attachmentBlockReason([item({ status: "error", asset: undefined })], true)).toBe(
      "有图片上传失败，重试或移除后再发送",
    );
  });

  test("上传中和失败同时存在：先提示失败（需要用户动手的优先）", () => {
    const items = [
      item({ id: "a", status: "uploading", asset: undefined }),
      item({ id: "b", status: "error", asset: undefined }),
    ];
    expect(attachmentBlockReason(items, true)).toBe("有图片上传失败，重试或移除后再发送");
  });

  test("当前模型不能看图而托盘里有图：拦，提示换模型或移除", () => {
    expect(attachmentBlockReason([item()], false)).toBe(
      "当前模型看不到图片，换一个能看图的模型或移除图片",
    );
  });

  test("当前模型不能看图但托盘是空的：不拦", () => {
    expect(attachmentBlockReason([], false)).toBeNull();
  });
});

describe("附件托盘：发给后端的内容", () => {
  test("只带已上传图片的素材 id，按托盘顺序", () => {
    const items = [
      item({
        id: "a",
        asset: { id: "11", url: "/files/1", width: null, height: null, mimeType: "image/png" },
      }),
      item({
        id: "b",
        asset: { id: "22", url: "/files/2", width: null, height: null, mimeType: "image/jpeg" },
      }),
    ];
    expect(toAttachmentsPayload(items)).toEqual([{ asset_id: "11" }, { asset_id: "22" }]);
  });

  test("没有上传完成的图片不会混进去", () => {
    expect(toAttachmentsPayload([item({ status: "uploading", asset: undefined })])).toEqual([]);
  });

  test("同一个素材重复出现只发一次", () => {
    expect(toAttachmentsPayload([item({ id: "a" }), item({ id: "b" })])).toEqual([
      { asset_id: "101" },
    ]);
  });
});

describe("附件托盘：一条消息最多 9 张", () => {
  test("没超：全收", () => {
    expect(clipToLimit(0, 3)).toEqual({ accept: 3, clipped: 0 });
  });

  test("托盘里已有一部分：只收剩下的名额，多出的记在 clipped", () => {
    expect(clipToLimit(MAX_ATTACH_IMAGES - 2, 5)).toEqual({ accept: 2, clipped: 3 });
  });

  test("已经满了：一张都不收", () => {
    expect(clipToLimit(MAX_ATTACH_IMAGES, 2)).toEqual({ accept: 0, clipped: 2 });
  });
});
