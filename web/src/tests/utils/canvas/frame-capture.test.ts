import { describe, expect, test } from "bun:test";

import {
  FrameCaptureError,
  captureUrl,
  clampFrameTime,
  formatFrameTime,
  frameFileName,
  lastFrameTime,
  openFrameReader,
  scaledSize,
  type FrameCanvas,
  type FrameDeps,
  type FrameVideo,
} from "@/utils/canvas/frame-capture";

describe("时间与尺寸换算", () => {
  test("尾帧取时长往前一点点：正好等于时长常常取不到画面", () => {
    expect(lastFrameTime(8)).toBeLessThan(8);
    expect(lastFrameTime(8)).toBeGreaterThan(7.9);
    expect(lastFrameTime(0.01)).toBe(0);
  });

  test("时刻限制在 0 到尾帧之间", () => {
    expect(clampFrameTime(-3, 8)).toBe(0);
    expect(clampFrameTime(99, 8)).toBe(lastFrameTime(8));
    expect(clampFrameTime(4.5, 8)).toBe(4.5);
  });

  test("时间显示成 mm:ss，向下取整", () => {
    expect(formatFrameTime(4)).toBe("00:04");
    expect(formatFrameTime(4.9)).toBe("00:04");
    expect(formatFrameTime(65)).toBe("01:05");
  });

  test("文件名：首帧、尾帧用文字，自定义用时刻，冒号等不能出现在文件名里的字符被替换", () => {
    expect(frameFileName("镜头10", "first")).toBe("镜头10-首帧.png");
    expect(frameFileName("镜头10", "last")).toBe("镜头10-尾帧.png");
    expect(frameFileName("镜头10", 4)).toBe("镜头10-00-04.png");
    expect(frameFileName('a/b:c*"?', "first")).toBe("a_b_c___-首帧.png");
  });

  test("尺寸按最长边封顶，不放大", () => {
    expect(scaledSize(1920, 1080, 160)).toEqual({ width: 160, height: 90 });
    expect(scaledSize(1080, 1920, 160)).toEqual({ width: 90, height: 160 });
    expect(scaledSize(640, 360, 4096)).toEqual({ width: 640, height: 360 });
    expect(scaledSize(1, 1000, 100)).toEqual({ width: 1, height: 100 });
  });

  test("只有后端 /files/ 地址追加 cap 参数，防止命中不带跨域头的缓存；外链原样", () => {
    expect(captureUrl("/files/a/b.mp4")).toBe("/files/a/b.mp4?cap=1");
    expect(captureUrl("https://x.com/files/a.mp4?v=poster")).toBe(
      "https://x.com/files/a.mp4?v=poster&cap=1",
    );
    expect(captureUrl("https://cdn.example.com/a.mp4?sig=1")).toBe(
      "https://cdn.example.com/a.mp4?sig=1",
    );
    expect(captureUrl("blob:abc")).toBe("blob:abc");
  });
});

/** 假 video：src 一设就按脚本触发事件，currentTime 一改就触发 seeked */
function fakeVideo(options: {
  duration?: number;
  width?: number;
  height?: number;
  loadEvent?: "loadeddata" | "error" | "never";
  seek?: "ok" | "never";
}) {
  const listeners = new Map<string, Set<() => void>>();
  const emit = (type: string) => listeners.get(type)?.forEach((fn) => fn());
  const state = { time: 0, loadCalls: 0, removed: [] as string[], seeks: [] as number[] };
  const video: FrameVideo = {
    crossOrigin: null,
    muted: false,
    preload: "",
    playsInline: false,
    duration: options.duration ?? 8,
    videoWidth: options.width ?? 1920,
    videoHeight: options.height ?? 1080,
    readyState: 0,
    get currentTime() {
      return state.time;
    },
    set currentTime(value: number) {
      state.time = value;
      state.seeks.push(value);
      if (options.seek !== "never") queueMicrotask(() => emit("seeked"));
    },
    set src(_value: string) {
      const event = options.loadEvent ?? "loadeddata";
      if (event === "never") return;
      queueMicrotask(() => {
        if (event === "loadeddata") video.readyState = 2;
        emit(event);
      });
    },
    addEventListener: (type, fn) => {
      const set = listeners.get(type) ?? new Set();
      set.add(fn);
      listeners.set(type, set);
    },
    removeEventListener: (type, fn) => listeners.get(type)?.delete(fn),
    removeAttribute: (name) => void state.removed.push(name),
    load: () => void (state.loadCalls += 1),
    paused: true,
    ended: false,
    play: async () => {
      video.paused = false;
    },
    pause: () => {
      video.paused = true;
    },
  } as FrameVideo;
  return { video, state };
}

function fakeCanvas(options: { toBlob?: "ok" | "taint" | "null" } = {}) {
  const draws: number[][] = [];
  const canvas = {
    width: 0,
    height: 0,
    getContext: () => ({
      drawImage: (_v: unknown, x: number, y: number, w: number, h: number) =>
        void draws.push([x, y, w, h]),
    }),
    toBlob: (cb: (blob: Blob | null) => void, type?: string) => {
      if (options.toBlob === "taint") throw new DOMException("tainted", "SecurityError");
      cb(options.toBlob === "null" ? null : new Blob(["x"], { type }));
    },
  } as unknown as FrameCanvas;
  return { canvas, draws };
}

const deps = (video: FrameVideo, canvas: FrameCanvas): FrameDeps => ({
  createVideo: () => video,
  createCanvas: () => canvas,
});

describe("openFrameReader / grab", () => {
  test("带跨域读取、静音，读出时长和画面尺寸", async () => {
    const { video } = fakeVideo({ duration: 8, width: 1280, height: 720 });
    const reader = await openFrameReader("/files/a.mp4", deps(video, fakeCanvas().canvas));
    expect(video.crossOrigin).toBe("anonymous");
    expect(video.muted).toBe(true);
    expect(reader.duration).toBe(8);
    expect(reader.size).toEqual({ width: 1280, height: 720 });
  });

  test("截指定时刻：定位后画出整帧，按原尺寸导出 PNG 文件", async () => {
    const { video, state } = fakeVideo({ width: 1280, height: 720 });
    const { canvas, draws } = fakeCanvas();
    const reader = await openFrameReader("/files/a.mp4", deps(video, canvas));
    const file = await reader.grab(4, { fileName: "帧.png" });
    expect(state.seeks).toEqual([4]);
    expect(draws).toEqual([[0, 0, 1280, 720]]);
    expect(file).toBeInstanceOf(File);
    expect(file.name).toBe("帧.png");
    expect(file.type).toBe("image/png");
  });

  test("最长边封顶：缩略图按宽度缩小后导出 JPEG", async () => {
    const { video } = fakeVideo({ width: 1920, height: 1080 });
    const { canvas, draws } = fakeCanvas();
    const reader = await openFrameReader("/files/a.mp4", deps(video, canvas));
    const file = await reader.grab(2, { maxEdge: 160, type: "image/jpeg", fileName: "t.jpg" });
    expect(draws).toEqual([[0, 0, 160, 90]]);
    expect(file.type).toBe("image/jpeg");
  });

  test("尾帧：用「last」取时长往前一点", async () => {
    const { video, state } = fakeVideo({ duration: 8 });
    const reader = await openFrameReader("/files/a.mp4", deps(video, fakeCanvas().canvas));
    await reader.grab("last", { fileName: "x.png" });
    expect(state.seeks).toEqual([lastFrameTime(8)]);
  });

  test("要截的时刻就是当前位置时不再等 seeked，直接画", async () => {
    const { video, state } = fakeVideo({ seek: "never" });
    const reader = await openFrameReader("/files/a.mp4", deps(video, fakeCanvas().canvas));
    await reader.grab(0, { fileName: "x.png" });
    expect(state.seeks).toEqual([]);
  });

  test("多次截取排队执行，互不抢同一个 video", async () => {
    const { video, state } = fakeVideo({});
    const reader = await openFrameReader("/files/a.mp4", deps(video, fakeCanvas().canvas));
    await Promise.all([
      reader.grab(1, { fileName: "a.png" }),
      reader.grab(3, { fileName: "b.png" }),
      reader.grab(5, { fileName: "c.png" }),
    ]);
    expect(state.seeks).toEqual([1, 3, 5]);
  });

  test("dispose 释放视频资源，之后再截会报错", async () => {
    const { video, state } = fakeVideo({});
    const reader = await openFrameReader("/files/a.mp4", deps(video, fakeCanvas().canvas));
    reader.dispose();
    expect(state.removed).toContain("src");
    expect(state.loadCalls).toBe(1);
    await expect(reader.grab(1, { fileName: "a.png" })).rejects.toBeInstanceOf(FrameCaptureError);
  });
});

describe("截帧失败", () => {
  test("视频加载出错：读不出画面（没配跨域或格式不支持）", async () => {
    const { video } = fakeVideo({ loadEvent: "error" });
    const error = await openFrameReader("/files/a.mp4", deps(video, fakeCanvas().canvas)).catch(
      (e) => e,
    );
    expect(error).toBeInstanceOf(FrameCaptureError);
    expect(error.code).toBe("unreadable");
  });

  test("一直不出画面：超时", async () => {
    const { video } = fakeVideo({ loadEvent: "never" });
    const error = await openFrameReader("/files/a.mp4", deps(video, fakeCanvas().canvas), {
      timeoutMs: 20,
    }).catch((e) => e);
    expect(error.code).toBe("timeout");
  });

  test("定位后一直没有 seeked：超时，并且之后的排队任务不被卡死", async () => {
    const { video } = fakeVideo({ seek: "never" });
    const reader = await openFrameReader("/files/a.mp4", deps(video, fakeCanvas().canvas), {
      timeoutMs: 20,
    });
    const error = await reader.grab(3, { fileName: "a.png" }).catch((e) => e);
    expect(error.code).toBe("timeout");
    // 超时那次已经把 currentTime 改到了 3，再截同一时刻不用定位，队列能继续往下走
    await expect(reader.grab(3, { fileName: "b.png" })).resolves.toBeInstanceOf(File);
  });

  test("画布被污染（存储没开跨域读取）：导出时抛 SecurityError，翻译成 cors", async () => {
    const { video } = fakeVideo({});
    const reader = await openFrameReader(
      "/files/a.mp4",
      deps(video, fakeCanvas({ toBlob: "taint" }).canvas),
    );
    const error = await reader.grab(2, { fileName: "a.png" }).catch((e) => e);
    expect(error.code).toBe("cors");
    expect(error.message).toContain("跨域");
  });

  test("导出得到空结果：按读不出画面处理", async () => {
    const { video } = fakeVideo({});
    const reader = await openFrameReader(
      "/files/a.mp4",
      deps(video, fakeCanvas({ toBlob: "null" }).canvas),
    );
    const error = await reader.grab(2, { fileName: "a.png" }).catch((e) => e);
    expect(error.code).toBe("unreadable");
  });

  test("时长未知（直播流等）：直接报错，不去截尾帧", async () => {
    const { video } = fakeVideo({ duration: Number.POSITIVE_INFINITY });
    const error = await openFrameReader("/files/a.mp4", deps(video, fakeCanvas().canvas)).catch(
      (e) => e,
    );
    expect(error.code).toBe("unreadable");
  });
});

describe("截帧器：预览用的定位、播放、绘制", () => {
  test("seek 排进同一个队列，定位完再返回，当前时刻可读", async () => {
    const { video, state } = fakeVideo({});
    const reader = await openFrameReader("/files/a.mp4", deps(video, fakeCanvas().canvas));
    await reader.seek(3);
    expect(state.seeks).toEqual([3]);
    expect(reader.time()).toBe(3);
  });

  test("seek 的时刻限制在 0 到尾帧之间", async () => {
    const { video, state } = fakeVideo({ duration: 8 });
    const reader = await openFrameReader("/files/a.mp4", deps(video, fakeCanvas().canvas));
    await reader.seek(99);
    expect(state.seeks).toEqual([lastFrameTime(8)]);
  });

  test("drawTo 把当前画面画满目标画布，不重新编码", async () => {
    const { video } = fakeVideo({});
    const reader = await openFrameReader("/files/a.mp4", deps(video, fakeCanvas().canvas));
    const target = fakeCanvas();
    target.canvas.width = 640;
    target.canvas.height = 360;
    reader.drawTo(target.canvas);
    expect(target.draws).toEqual([[0, 0, 640, 360]]);
  });

  test("播放、暂停，playing 反映真实状态", async () => {
    const { video } = fakeVideo({});
    const reader = await openFrameReader("/files/a.mp4", deps(video, fakeCanvas().canvas));
    expect(reader.playing()).toBe(false);
    await reader.play();
    expect(reader.playing()).toBe(true);
    reader.pause();
    expect(reader.playing()).toBe(false);
  });
});
