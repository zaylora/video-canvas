import type { TaskView } from "@/api/generation-task/type";
import service from "@/utils/requests/service";
import type {
  ConversationDto,
  GenerateKind,
  RecordDto,
  RecordPageDto,
  RecordSubmitErrorDto,
  SubmitRecordRequest,
  SubmitRecordResultDto,
  SubmitTarget,
} from "./type";

/** 后端对话（snake_case） */
interface BackendConversationDto {
  id: string;
  title: string;
  is_default: boolean;
  record_count: number;
  last_record_at: string | null;
  active: boolean;
  created_at: string;
}

/** 后端记录（snake_case） */
interface BackendRecordDto {
  id: string;
  conversation_id: string;
  kind: GenerateKind;
  model_id: string;
  prompt: string;
  input: Record<string, unknown> | null;
  count: number;
  tasks: Array<TaskView | null> | null;
  submit_errors: RecordSubmitErrorDto[] | null;
  quote_credits: number;
  created_at: string;
}

/** 后端一页记录 */
interface BackendRecordPageDto {
  items: BackendRecordDto[] | null;
  next: string | null;
}

/** 后端提交结果 */
interface BackendSubmitResultDto {
  conversation_id: string;
  record: BackendRecordDto;
}

const mapConversation = (raw: BackendConversationDto): ConversationDto => ({
  id: raw.id,
  title: raw.title,
  isDefault: raw.is_default,
  recordCount: raw.record_count,
  lastRecordAt: raw.last_record_at,
  active: raw.active,
  createdAt: raw.created_at,
});

/** 后端记录 -> 前端记录；null 的数组兜底成空数组，tasks 长度补齐到格子数 */
export const mapRecord = (raw: BackendRecordDto): RecordDto => {
  const tasks = raw.tasks ?? [];
  return {
    id: raw.id,
    conversationId: raw.conversation_id,
    kind: raw.kind,
    modelId: raw.model_id,
    prompt: raw.prompt,
    input: raw.input ?? {},
    count: raw.count,
    tasks: Array.from({ length: raw.count }, (_, index) => tasks[index] ?? null),
    submitErrors: raw.submit_errors ?? [],
    quoteCredits: raw.quote_credits,
    createdAt: raw.created_at,
  };
};

/**
 * 获取对话列表：默认创作永远第一条，其余按最近记录时间倒序
 * @returns 对话列表
 */
export const getConversations = async (): Promise<ConversationDto[]> =>
  ((await service.get<BackendConversationDto[] | null>("/conversations")) ?? []).map(
    mapConversation,
  );

/**
 * 新建一段对话
 * @param title 标题，不传叫「新对话」
 * @returns 新对话
 */
export const createConversation = async (title?: string): Promise<ConversationDto> =>
  mapConversation(await service.post<BackendConversationDto>("/conversations", { title }));

/**
 * 重命名对话
 * @param id 对话 ID
 * @param title 新标题，1–50 字
 */
export const renameConversation = (id: string, title: string) =>
  service.patch<null>(`/conversations/${id}`, { title });

/**
 * 删除对话（软删除）；默认创作不能删。对话里进行中的任务继续跑完，素材仍在资产里
 * @param id 对话 ID
 */
export const deleteConversation = (id: string) => service.delete<null>(`/conversations/${id}`);

/**
 * 分页取对话里的记录，每页按新到旧
 * @param id 对话 ID
 * @param params before 为上一页返回的游标，不传取最新一页；limit 默认 20
 * @returns 一页记录
 */
export const getConversationRecords = async (
  id: string,
  params: { before?: string; limit?: number } = {},
): Promise<RecordPageDto> => {
  const page = await service.get<BackendRecordPageDto>(`/conversations/${id}/records`, params);
  return { items: (page.items ?? []).map(mapRecord), next: page.next ?? null };
};

/**
 * 提交一条生成记录（HTTP 202）。同一次点击的重试必须复用同一个 idempotencyKey，
 * 后端据此去重，重复提交只会得到同一条记录。
 * @param target 提交到默认创作、新建一段对话，或某段对话
 * @param data 提交内容
 * @param idempotencyKey 幂等键
 * @returns 记录和所在对话
 */
export const submitConversationRecord = async (
  target: SubmitTarget,
  data: SubmitRecordRequest,
  idempotencyKey: string,
): Promise<SubmitRecordResultDto> => {
  const result = await service.post<BackendSubmitResultDto>(
    `/conversations/${target}/records`,
    {
      kind: data.kind,
      model_id: data.modelId,
      prompt: data.prompt,
      input: data.input,
      count: data.count,
      title: data.title,
    },
    { headers: { "Idempotency-Key": idempotencyKey } },
  );
  return { conversationId: result.conversation_id, record: mapRecord(result.record) };
};

/**
 * 删除一条记录
 * @param conversationId 对话 ID
 * @param recordId 记录 ID
 * @param cancelActive 为 true 时先取消其中进行中的任务并退回积分；否则任务继续跑完
 */
export const deleteConversationRecord = (
  conversationId: string,
  recordId: string,
  cancelActive: boolean,
) =>
  service.delete<null>(
    `/conversations/${conversationId}/records/${recordId}${cancelActive ? "?cancel_active=true" : ""}`,
  );
