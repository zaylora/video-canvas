import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { NodeVideoBody } from "@/components/canvas/node-video-body";
import type { VideoNodeView } from "@/utils/tasks/node-view";

const render = (view: VideoNodeView, props: Partial<Parameters<typeof NodeVideoBody>[0]> = {}) =>
  renderToStaticMarkup(<NodeVideoBody view={view} placeholder="占位" {...props} />);

describe("上传中的节点正文", () => {
  test("显示「上传中」、百分比和进度条", () => {
    const out = render({ phase: "uploading", progress: 37 }, { mediaType: "image" });
    expect(out).toContain("上传中");
    expect(out).toContain("37%");
    expect(out).toContain("width:37%");
  });

  test("字节传完（100%）后显示「处理中」，说明服务端还在收尾", () => {
    const out = render({ phase: "uploading", progress: 100 });
    expect(out).toContain("处理中");
  });

  test("不给取消按钮：取消靠删除节点", () => {
    expect(render({ phase: "uploading", progress: 5 })).not.toContain("取消");
  });
});

describe("成功的节点正文", () => {
  test("素材下面不再显示文件名", () => {
    // 老调用方可能还在传 caption，传了也不能渲染出来
    const props = { caption: "镜头8.mp4" } as never;
    const out = render({ phase: "done", src: "https://example.com/a.mp4" }, props);
    expect(out).not.toContain("镜头8.mp4");
  });
});

describe("上传失败的节点正文", () => {
  const failed: VideoNodeView = {
    phase: "failed",
    message: "上传失败，请重试",
    refunded: false,
    taskRef: null,
  };

  test("给了重试就显示重试按钮，没给就不显示（刷新后文件已经丢了）", () => {
    expect(render(failed, { onRetry: () => {} })).toContain("重试");
    expect(render(failed)).not.toContain("<button");
  });

  test("不是生成任务，不提积分退回", () => {
    expect(render(failed, { onRetry: () => {} })).not.toContain("积分已退回");
  });
});

describe("节点框画幅跟着 aspect 走", () => {
  const PORTRAIT = 9 / 16;
  const phases: Array<[string, VideoNodeView]> = [
    ["还没生成的占位框", { phase: "idle" }],
    ["排队中", { phase: "queued" }],
    ["生成中", { phase: "running", progress: null, elapsedMs: null }],
    ["上传中", { phase: "uploading", progress: 10 }],
    ["即将完成", { phase: "finalizing" }],
    ["成片", { phase: "done", src: "https://example.com/a.mp4" }],
    ["失败", { phase: "failed", message: "x", refunded: false, taskRef: null }],
  ];

  for (const [name, view] of phases) {
    test(`${name}按给定画幅撑高`, () => {
      // 图片成片要挂在 ReactFlowProvider 里才能渲染，这里用视频走成片分支
      const mediaType = view.phase === "done" ? "video" : "image";
      const out = render(view, { aspect: PORTRAIT, mediaType });
      expect(out).toContain(`aspect-ratio:${PORTRAIT}`);
      expect(out).not.toContain(`aspect-ratio:${16 / 9}`);
    });
  }

  test("不给 aspect 时仍是默认的 16:9", () => {
    expect(render({ phase: "queued" })).toContain(`aspect-ratio:${16 / 9}`);
  });
});
