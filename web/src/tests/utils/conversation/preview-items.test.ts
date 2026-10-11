import { describe, expect, test } from "bun:test";

import type { RecordDto } from "@/api/conversation/type";
import type { TaskOutput, TaskStatus, TaskView } from "@/api/generation-task/type";
import { recordPreviewItems } from "@/utils/conversation/preview-items";

const output = (url: string | null, media_type = "image"): TaskOutput =>
  ({ url, media_type }) as unknown as TaskOutput;

const task = (id: number, status: TaskStatus, outputs: TaskOutput[] | null): TaskView =>
  ({ id, status, outputs }) as unknown as TaskView;

const record = (prompt = "小女孩在跳舞"): RecordDto => ({ prompt }) as unknown as RecordDto;

describe("recordPreviewItems：一条记录里能放进灯箱的结果", () => {
  test("只收成功且有地址的图片和视频，其余格子不进", () => {
    const items = recordPreviewItems(record(), [
      task(1, "succeeded", [output("/files/a.png")]),
      task(2, "running", null),
      task(3, "failed", null),
      task(4, "succeeded", [output("/files/v.mp4", "video")]),
      task(5, "succeeded", [output("/files/s.mp3", "audio")]),
      task(6, "succeeded", [output(null)]),
    ]);
    expect(items.map((item) => item.id)).toEqual(["1", "4"]);
    expect(items.map((item) => item.mediaType)).toEqual(["image", "video"]);
  });

  test("顺序和格子一致，id 用任务 ID（和结果格子认任务的方式相同）", () => {
    const items = recordPreviewItems(record(), [
      task(9, "succeeded", [output("/files/9.png")]),
      task(7, "succeeded", [output("/files/7.png")]),
    ]);
    expect(items.map((item) => item.id)).toEqual(["9", "7"]);
    expect(items.map((item) => item.src)).toEqual(["/files/9.png", "/files/7.png"]);
  });

  test("文件名用提示词前 20 字加第几格的序号，和结果格子上的下载一致", () => {
    const items = recordPreviewItems(record("一二三四五六七八九十一二三四五六七八九十多出来的"), [
      task(1, "succeeded", [output("/files/a.png")]),
      task(2, "failed", null),
      task(3, "succeeded", [output("/files/b.png")]),
    ]);
    expect(items.map((item) => item.label)).toEqual([
      "一二三四五六七八九十一二三四五六七八九十-1",
      "一二三四五六七八九十一二三四五六七八九十-3",
    ]);
  });

  test("提示词为空时用「生成结果」", () => {
    const [item] = recordPreviewItems(record(""), [task(1, "succeeded", [output("/files/a.png")])]);
    expect(item?.label).toBe("生成结果-1");
  });

  test("没有任何可预览结果时返回空数组", () => {
    expect(recordPreviewItems(record(), [])).toEqual([]);
  });
});
