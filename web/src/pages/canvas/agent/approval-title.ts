import type {
  AgentApprovalDto,
  AgentAskPayload,
  AgentDeletePayload,
  AgentGeneratePayload,
  AdminSecretPayload,
  AdminTestRunPayload,
} from "@/api/agent/type";
import { adminChangeTitle } from "@/utils/admin/assistant/change-order";
import { editPayload, editTitle } from "@/utils/agent/drama";

/** 删除审批的内容；服务端的空列表可能是 null（旧数据），统一补成空数组 */
export function deletePayload(raw: unknown): AgentDeletePayload {
  const p = (raw ?? {}) as Partial<AgentDeletePayload>;
  return {
    reason: p.reason ?? "",
    node_ids: p.node_ids ?? [],
    labels: p.labels ?? [],
    outputs: p.outputs ?? [],
    edge_ids: p.edge_ids ?? [],
  };
}

/** 审批的一句话标题：交互卡片和记录行共用 */
export function approvalTitle(approval: AgentApprovalDto) {
  switch (approval.kind) {
    case "generate":
      return `申请生成 ${(approval.payload as AgentGeneratePayload).items.length} 个节点`;
    case "delete": {
      const p = deletePayload(approval.payload);
      return `申请删除 ${p.node_ids.length} 个节点${p.edge_ids.length ? `、${p.edge_ids.length} 条连线` : ""}`;
    }
    case "ask":
      return (approval.payload as AgentAskPayload).question;
    // 后台助手的变更单
    case "admin_change":
      return adminChangeTitle(approval);
    case "admin_secret":
      return `填写密钥：${(approval.payload as AdminSecretPayload).channel_label}`;
    case "admin_test_run":
      return `申请试跑：${(approval.payload as AdminTestRunPayload).model_label}`;
    // 短剧助手的修改审批；不认识的种类也按它显示
    default:
      return editTitle(editPayload(approval.payload));
  }
}
