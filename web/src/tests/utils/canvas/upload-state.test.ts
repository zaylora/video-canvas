import { describe, expect, test } from "bun:test";

import type { CanvasNode } from "@/types";
import {
  deserializeGraph,
  hasVolatileRunning,
  serializeGraph,
} from "@/utils/canvas/canvas-persistence";
import { canLinkFrom, canLinkNodes, partitionLinkable } from "@/utils/canvas/link-rule";
import { isUploadFailed, isUploading } from "@/utils/canvas/upload-state";

describe("isUploading：节点是不是正在上传素材", () => {
  test("生成中带上传进度才算上传中", () => {
    expect(isUploading({ status: "running", uploadProgress: 0 })).toBe(true);
    expect(isUploading({ status: "running", uploadProgress: 63 })).toBe(true);
  });

  test("普通的生成中（没有上传进度）不算", () => {
    expect(isUploading({ status: "running" })).toBe(false);
  });

  test("上传完成或失败后不再算，哪怕进度字段还在", () => {
    expect(isUploading({ status: "done", uploadProgress: 100 })).toBe(false);
    expect(isUploading({ status: "error", uploadProgress: 40 })).toBe(false);
    expect(isUploading({})).toBe(false);
  });
});

describe("isUploadFailed：失败的是上传还是生成", () => {
  test("上传来的素材失败且从没入过库，是上传失败", () => {
    expect(isUploadFailed({ status: "error", uploaded: true })).toBe(true);
  });

  test("已经入过库（有素材 id）的节点再失败，是生成失败", () => {
    expect(isUploadFailed({ status: "error", uploaded: true, assetId: "3" })).toBe(false);
  });

  test("不是上传来的、或者没失败，都不算", () => {
    expect(isUploadFailed({ status: "error" })).toBe(false);
    expect(isUploadFailed({ status: "done", uploaded: true })).toBe(false);
  });
});

describe("上传中的节点不能被引用", () => {
  const uploading = { kind: "image" as const, status: "running" as const, uploadProgress: 10 };
  const done = { kind: "image" as const, status: "done" as const };

  test("接不上任何下游：不能拉线出去，也不能落到别的节点上", () => {
    expect(canLinkNodes(uploading, { kind: "video" })).toBe(false);
    expect(canLinkNodes(uploading, { kind: "image" })).toBe(false);
    expect(canLinkFrom(uploading, "source", { kind: "video" })).toBe(false);
  });

  test("上传完成后恢复正常", () => {
    expect(canLinkNodes(done, { kind: "video" })).toBe(true);
  });

  test("多选引用时被跳过，其余照常接", () => {
    const { linkable, skipped } = partitionLinkable(
      [
        { id: "up", ...uploading },
        { id: "ok", ...done },
      ],
      { kind: "video" },
    );
    expect(linkable.map((item) => item.id)).toEqual(["ok"]);
    expect(skipped.map((item) => item.id)).toEqual(["up"]);
  });
});

describe("保存：上传中的节点不存，也不挡着别的改动保存", () => {
  const node = (id: string, data: Partial<CanvasNode["data"]>): CanvasNode => ({
    id,
    type: "canvas",
    position: { x: 0, y: 0 },
    data: { kind: "image", label: id, ...data },
  });
  const viewport = { x: 0, y: 0, zoom: 1 };

  test("序列化时丢掉上传中的节点，其余原样保留", () => {
    const graph = serializeGraph(
      [
        node("up", { status: "running", uploadProgress: 30, uploaded: true }),
        node("ok", { status: "done", src: "/a.png", assetId: "1" }),
      ],
      [],
      viewport,
    );
    expect(graph.nodes.map((item) => item.id)).toEqual(["ok"]);
  });

  test("上传中不算「要等它收尾」的本地生成中，不会拖住保存", () => {
    expect(hasVolatileRunning([node("up", { status: "running", uploadProgress: 5 })])).toBe(false);
    expect(hasVolatileRunning([node("gen", { status: "running" })])).toBe(true);
  });

  test("上传失败的节点照常保存，读回来还是失败态", () => {
    const failed = node("bad", { status: "error", uploaded: true, error: "上传失败，请重试" });
    const graph = serializeGraph([failed], [], viewport);
    expect(graph.nodes).toHaveLength(1);
    expect(deserializeGraph(graph).nodes[0].data).toMatchObject({ status: "error" });
  });
});
