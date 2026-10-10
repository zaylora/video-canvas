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
