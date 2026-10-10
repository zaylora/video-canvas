import { describe, expect, test } from "bun:test";

import { UPLOAD_ACCEPT, UPLOAD_SIZE_LIMIT, UPLOAD_TARGET_KIND } from "@/constants/canvas";
import { takeUploadFile } from "@/utils/canvas/media";

/** 造一个指定类型和大小的文件，内容用不着，只看 type 和 size */
function fileOf(type: string, size = 1, name = "x") {
  const file = new File(["a"], name, { type });
  if (size !== 1) Object.defineProperty(file, "size", { value: size });
  return file;
}

describe("takeUploadFile：上传时认下的文件种类", () => {
  test("图片、视频、音频都认下，各自标好种类", () => {
    expect(takeUploadFile(fileOf("image/png"))).toMatchObject({ mediaType: "image" });
    expect(takeUploadFile(fileOf("video/mp4"))).toMatchObject({ mediaType: "video" });
    expect(takeUploadFile(fileOf("audio/mpeg"))).toMatchObject({ mediaType: "audio" });
  });

  test("其他类型不收，原因里写明收哪三类", () => {
    const result = takeUploadFile(fileOf("application/pdf"));
    expect(result).toEqual({ error: "只收图片、视频和音频，换个文件试试" });
  });

  test("音频超过上限就拦下，并说明上限", () => {
    const limit = UPLOAD_SIZE_LIMIT.audio;
    expect(limit).toBe(50 * 1024 * 1024);
    expect(takeUploadFile(fileOf("audio/wav", limit))).toMatchObject({ mediaType: "audio" });
    expect(takeUploadFile(fileOf("audio/wav", limit + 1))).toEqual({
      error: "文件超过 50MB，换个小点的",
    });
  });
});

describe("上传相关常量", () => {
  test("文件框接受音频，音频落成音频节点", () => {
    expect(UPLOAD_ACCEPT.split(",")).toContain("audio/*");
    expect(UPLOAD_TARGET_KIND.audio).toBe("audio");
  });
});
