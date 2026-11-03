import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { AdminCanvasScope } from "@/pages/admin/assistant/admin-assistant-panel";
import { ActivityGroup } from "@/pages/canvas/agent/activity-group";
import type { TimelineItem } from "@/utils/agent/timeline";

type Activity = Extract<TimelineItem, { type: "activity" }>;

/** 后台助手的一组工具调用：查模型、查定价设置 */
const activity: Activity = {
  type: "activity",
  key: "gt1",
  runId: "r1",
  groupId: "c1",
  tools: [
    { key: "t1", name: "admin_list_models", status: "done", summary: "", nodeIds: [] },
    { key: "t2", name: "admin_get_pricing_settings", status: "running", summary: "", nodeIds: [] },
  ],
  status: "running",
  errors: 0,
  startedAt: 0,
  endedAt: null,
};

/**
 * 后台没有画布，但复用的画布 Agent 组件（工具行、节点引用、改动摘要）会取 ReactFlow 的 hook。
 * 后台助手面板外面包一层空的画布作用域，这些组件才能渲染；定位节点在后台找不到节点，按钮是禁用的。
 */
describe("后台助手的画布作用域", () => {
  test("工具活动组在作用域里能渲染", () => {
    expect(() =>
      renderToStaticMarkup(
        <AdminCanvasScope>
          <ActivityGroup item={activity} />
        </AdminCanvasScope>,
      ),
    ).not.toThrow();
  });

  test("没有作用域时会报 ReactFlowProvider 错误（说明这层作用域是必需的）", () => {
    expect(() => renderToStaticMarkup(<ActivityGroup item={activity} />)).toThrow(
      /ReactFlowProvider/,
    );
  });
});
