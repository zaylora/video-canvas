import { describe, expect, test } from "bun:test";
import { ReactFlowProvider } from "@xyflow/react";
import { renderToStaticMarkup } from "react-dom/server";

import { ActivityGroup } from "@/pages/canvas/agent/activity-group";
import type { TimelineItem, ToolCall } from "@/utils/agent/timeline";

type Activity = Extract<TimelineItem, { type: "activity" }>;

const tool = (name: string, extra: Partial<ToolCall> = {}): ToolCall => ({
  key: `t-${name}-${Math.random()}`,
  name,
  status: "done",
  summary: "",
  nodeIds: [],
  ...extra,
});
const block = (over: Partial<Activity> = {}): Activity => ({
  type: "activity",
  key: "gt1",
  runId: "r1",
  groupId: "c1",
  tools: [tool("canvas_get_state"), tool("canvas_apply_ops")],
  status: "done",
  errors: 0,
  startedAt: 0,
  endedAt: 12_000,
  ...over,
});
const html = (item: Activity) =>
  renderToStaticMarkup(
    <ReactFlowProvider>
      <ActivityGroup item={item} />
    </ReactFlowProvider>,
  );
/** 取出所有 lucide 图标的名字（class 里的 lucide-xxx） */
const iconsOf = (out: string) =>
  [...out.matchAll(/lucide-([a-z0-9-]+)/g)].map((m) => m[1]).filter((n) => n !== "icon");

describe("活动行（收起）：一句话说明 + 主图标 + 耗时", () => {
  test("模型的说明到了：显示说明，不再显示兜底短语", () => {
    const out = html(block({ summary: "读了画布并改了提示词" }));
    expect(out).toContain("读了画布并改了提示词");
    expect(out).not.toContain("读取了画布");
  });

  test("说明还没到：显示动作短语兜底", () => {
    expect(html(block())).toContain("读取了画布、修改了画布");
  });

  test("主图标是这一段优先级最高的动作，不再是对勾；耗时在右侧", () => {
    const out = html(block());
    expect(iconsOf(out)).toContain("pencil-line");
    expect(iconsOf(out)).not.toContain("circle-check");
    expect(out).toContain("12s");
    expect(html(block({ endedAt: 72_000 }))).toContain("1m 12s");
  });

  test("有失败的调用：写「N 项失败」", () => {
    expect(html(block({ errors: 1 }))).toContain("1 项失败");
  });

  test("已中止：停止图标 + 「已中止」，说明里带上动作短语", () => {
    const out = html(block({ status: "aborted", summary: "不该显示的说明" }));
    expect(iconsOf(out)).toContain("circle-stop");
    expect(out).toContain("已中止");
    expect(out).toContain("读取了画布");
    expect(out).not.toContain("不该显示的说明");
  });

  test("收起时不画每条操作", () => {
    expect(
      html(block({ tools: [tool("canvas_get_state", { summary: "画布目录（3 个节点）" })] })),
    ).not.toContain("画布目录（3 个节点）");
  });
});

describe("活动行（展开）：每条操作以动作图标打头", () => {
  const running = (tools: ToolCall[]) => block({ status: "running", tools });

  test("运行中自动展开：每行是动作图标 + 名称 + 摘要，成功的不再画对勾", () => {
    const out = html(
      running([
        tool("canvas_get_state", { summary: "画布目录（3 个节点）" }),
        tool("web_search", { summary: "5 条结果" }),
      ]),
    );
    expect(out).toContain("画布目录（3 个节点）");
    expect(iconsOf(out)).toEqual(expect.arrayContaining(["book-open", "search"]));
    expect(iconsOf(out)).not.toContain("check");
  });

  test("进行中的那条是转圈；失败的那条右侧写「失败」；中止的写「已中止」", () => {
    const out = html(
      running([
        tool("canvas_get_state", { status: "running" }),
        tool("generate_media", { status: "error", summary: "模型暂不可用" }),
        tool("canvas_arrange", { status: "aborted" }),
      ]),
    );
    expect(out).toContain("animate-spin");
    expect(out).toContain("失败");
    expect(out).toContain("已中止");
    expect(iconsOf(out)).toEqual(expect.arrayContaining(["wand-sparkles", "layout-grid"]));
  });

  test("涉及节点的操作可以定位：行尾有定位图标；没有节点的没有", () => {
    const withNode = html(running([tool("canvas_apply_ops", { nodeIds: ["n1"] })]));
    const without = html(running([tool("canvas_apply_ops")]));
    expect(iconsOf(withNode)).toContain("locate-fixed");
    expect(iconsOf(without)).not.toContain("locate-fixed");
  });

  test("截取视频帧用相机图标，和查看视频的胶片图标区分开", () => {
    const out = html(running([tool("canvas_capture_frame"), tool("canvas_inspect_video")]));
    expect(iconsOf(out)).toEqual(expect.arrayContaining(["camera", "film"]));
  });
});
