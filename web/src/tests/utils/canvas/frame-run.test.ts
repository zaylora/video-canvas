import { describe, expect, test } from "bun:test";

import { TrimError } from "@/utils/canvas/trim-run";
import { grabFrame, type FrameEngine } from "@/utils/canvas/frame-run";

const png = () => new Blob(["x"], { type: "image/png" });

describe("grabFrame 取一帧", () => {
  test("参数交给引擎：地址不改，包成 PNG 文件", async () => {
    const calls: Parameters<FrameEngine["frameAt"]>[] = [];
    const engine: FrameEngine = {
      frameAt: async (...args) => {
        calls.push(args);
        return png();
      },
    };
    const file = await grabFrame(engine, {
      src: "/files/a.mp4",
      spec: "last",
      fileName: "x-尾帧.png",
    });
    expect(calls[0][0]).toBe("/files/a.mp4");
    expect(calls[0][1]).toBe("last");
    expect(file.name).toBe("x-尾帧.png");
    expect(file.type).toBe("image/png");
  });

  test("引擎报 TrimError 就把写给用户的话原样抛出", async () => {
    const engine: FrameEngine = {
      frameAt: async () => {
        throw new TrimError("undecodable");
      },
    };
    await expect(
      grabFrame(engine, { src: "/files/a.mp4", spec: "first", fileName: "x.png" }),
    ).rejects.toThrow("读不出视频：视频格式不支持修剪");
  });

  test("未知错误翻成通用提示，不端出内部报错", async () => {
    const engine: FrameEngine = {
      frameAt: async () => {
        throw new Error("boom internal");
      },
    };
    await expect(
      grabFrame(engine, { src: "/files/a.mp4", spec: 3, fileName: "x.png" }),
    ).rejects.toThrow("截取失败，请稍后重试");
  });

  test("中止信号触发时抛 AbortError，不翻译", async () => {
    const controller = new AbortController();
    const engine: FrameEngine = {
      frameAt: async (_s, _p, _o, signal) => {
        controller.abort();
        if (signal.aborted) throw new DOMException("已中止", "AbortError");
        return png();
      },
    };
    await expect(
      grabFrame(engine, { src: "/files/a.mp4", spec: 1, fileName: "x.png" }, controller.signal),
    ).rejects.toMatchObject({ name: "AbortError" });
  });
});
