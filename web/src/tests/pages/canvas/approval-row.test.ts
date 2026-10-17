import { describe, expect, test } from "bun:test";

import type { AgentApprovalDto } from "@/api/agent/type";
import { approvalTitle } from "@/pages/canvas/agent/approval-row";

const del = (payload: unknown) => ({ kind: "delete", payload }) as unknown as AgentApprovalDto;

describe("审批标题", () => {
  test("只删节点时服务端的 edge_ids 可能是 null，不崩溃", () => {
    expect(approvalTitle(del({ node_ids: ["a", "b"], edge_ids: null }))).toBe("申请删除 2 个节点");
  });

  test("只删连线时节点列表可能是 null，不崩溃", () => {
    expect(approvalTitle(del({ node_ids: null, edge_ids: ["e1"] }))).toBe(
      "申请删除 0 个节点、1 条连线",
    );
  });
});
