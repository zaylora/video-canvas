import { beforeEach, describe, expect, test } from "bun:test";

import { useTrimStore } from "@/store/trim";
import {
  TrimError,
  createTrimRunner,
  describeTrimError,
  type TrimEngine,
  type TrimJob,
} from "@/utils/canvas/trim-run";

const job = (over: Partial<TrimJob> = {}): TrimJob => ({
  nodeId: "n1",
  src: "/files/a.mp4",
  start: 3,
  end: 12.5,
  duration: 29,
  discardAudio: false,
  sourceBytes: 10 * 1024 * 1024,
  fileName: "视频 1-剪辑.mp4",
  ...over,
});

/** 一次可控的编码：测试决定什么时候报进度、成功还是失败 */
function controlled() {
  const calls: {
    args: Parameters<TrimEngine["encode"]>[0];
    ok: (blob: Blob) => void;
    fail: (error: unknown) => void;
  }[] = [];
  const engine: TrimEngine = {
    probe: async () => ({ duration: 29, hasAudio: true, width: 1328, height: 720 }),
    thumbnails: async () => [],
    encode: (args) =>
      new Promise<Blob>((resolve, reject) => {
        calls.push({ args, ok: resolve, fail: reject });
        args.signal.addEventListener("abort", () =>
          reject(new DOMException("已中止", "AbortError")),
        );
      }),
  };
  return { calls, engine };
}

beforeEach(() => useTrimStore.getState().reset());

describe("修剪运行器：编码", () => {
  test("开始时进入编码中；参数交给引擎，地址换成修剪专用的", () => {
    useTrimStore.getState().enter("n1");
    const { engine, calls } = controlled();
    const runner = createTrimRunner(engine);
    void runner.start(job({ discardAudio: true }));
    expect(useTrimStore.getState().phase).toBe("encoding");
    expect(calls[0].args).toMatchObject({
      src: "/files/a.mp4?trim=1",
      start: 3,
      end: 12.5,
      discardAudio: true,
    });
    expect(runner.isActive("n1")).toBe(true);
  });

  test("引擎报进度（0 到 1）折成整数百分比写进仓库", () => {
    useTrimStore.getState().enter("n1");
    const { engine, calls } = controlled();
    void createTrimRunner(engine).start(job());
    calls[0].args.onProgress(0.126);
    expect(useTrimStore.getState().progress).toBe(12);
    calls[0].args.onProgress(1.4);
    expect(useTrimStore.getState().progress).toBe(100);
  });

  test("成功：返回按文件名包好的 File，类型取 Blob 的，不再占着编码", async () => {
    useTrimStore.getState().enter("n1");
    const { engine, calls } = controlled();
    const runner = createTrimRunner(engine);
    const promise = runner.start(job());
    calls[0].ok(new Blob(["x"], { type: "video/mp4" }));
    const result = await promise;
    expect(result.outcome).toBe("done");
    if (result.outcome !== "done") throw new Error("unreachable");
    expect(result.file.name).toBe("视频 1-剪辑.mp4");
    expect(result.file.type).toBe("video/mp4");
    expect(runner.isActive("n1")).toBe(false);
  });

  test("失败：回到编辑态并留下原因；选区交还给面板所以不丢", async () => {
    useTrimStore.getState().enter("n1");
    const { engine, calls } = controlled();
    const promise = createTrimRunner(engine).start(job());
    calls[0].fail(new TrimError("cors"));
    const result = await promise;
    expect(result.outcome).toBe("failed");
    const state = useTrimStore.getState();
    expect(state.phase).toBe("editing");
    expect(state.error).toContain("跨域");
  });

  test("体积预估超过上传上限：不开编码，直接失败并写原因", async () => {
    useTrimStore.getState().enter("n1");
    const { engine, calls } = controlled();
    const result = await createTrimRunner(engine).start(
      job({ sourceBytes: 500 * 1024 * 1024, start: 0.5, end: 28.5 }),
    );
    expect(result.outcome).toBe("failed");
    expect(calls).toHaveLength(0);
    expect(useTrimStore.getState().error).toContain("200MB");
    expect(useTrimStore.getState().phase).toBe("editing");
  });

  test("产物超过上传上限：编码完发现太大也是失败，不把大文件交出去", async () => {
    useTrimStore.getState().enter("n1");
    const { engine, calls } = controlled();
    const promise = createTrimRunner(engine, { maxBytes: 4 }).start(job({ sourceBytes: 3 }));
    calls[0].ok(new Blob(["123456789"], { type: "video/mp4" }));
    const result = await promise;
    expect(result.outcome).toBe("failed");
    expect(useTrimStore.getState().error).toContain("超过");
  });
});

describe("修剪运行器：中止", () => {
  test("abort：中止引擎，结果是 aborted，不写错误、不碰仓库的阶段", async () => {
    useTrimStore.getState().enter("n1");
    const { engine } = controlled();
    const runner = createTrimRunner(engine);
    const promise = runner.start(job());
    runner.abort("n1");
    const result = await promise;
    expect(result.outcome).toBe("aborted");
    expect(useTrimStore.getState().error).toBeNull();
    expect(runner.isActive("n1")).toBe(false);
  });

  test("abort 一个没在编码的节点：什么都不发生", () => {
    const { engine } = controlled();
    expect(() => createTrimRunner(engine).abort("nope")).not.toThrow();
  });

  test("同一时刻只编码一个：第二次 start 直接失败，不顶替第一个", async () => {
    useTrimStore.getState().enter("n1");
    const { engine, calls } = controlled();
    const runner = createTrimRunner(engine);
    void runner.start(job());
    const second = await runner.start(job({ nodeId: "n2" }));
    expect(second.outcome).toBe("failed");
    expect(calls).toHaveLength(1);
    expect(runner.isActive("n1")).toBe(true);
  });

  test("abortAll：离开画布时中止所有", async () => {
    useTrimStore.getState().enter("n1");
    const { engine } = controlled();
    const runner = createTrimRunner(engine);
    const promise = runner.start(job());
    runner.abortAll();
    expect((await promise).outcome).toBe("aborted");
  });
});

describe("错误翻成给用户看的话", () => {
  test("已知原因给对应的话", () => {
    expect(describeTrimError(new TrimError("unsupported"))).toContain("不支持");
    expect(describeTrimError(new TrimError("no-video"))).toContain("没有画面");
    expect(describeTrimError(new TrimError("undecodable"))).toContain("格式");
  });

  test("网络层的 TypeError（fetch 被跨域或断网拦下）当跨域 / 网络问题", () => {
    expect(describeTrimError(new TypeError("Failed to fetch"))).toContain("跨域");
  });

  test("其它错误兜底成通用话，不把内部报错原样端给用户", () => {
    const text = describeTrimError(new Error("some internal webcodecs error 0x1234"));
    expect(text).not.toContain("0x1234");
    expect(text).toContain("修剪失败");
  });
});
