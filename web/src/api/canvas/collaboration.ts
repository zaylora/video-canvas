import type { CanvasVersion } from "@/utils/canvas/collaboration-version";

import service from "@/utils/requests/service";
import type { CollaborationChange } from "@/utils/canvas/collaboration-graph";
import { mapCanvasProject } from ".";
import type { BackendCanvasProjectDto, CanvasModePreviewDto } from "./type";

/** 当前用户在协作画布上的角色。 */
export type CollaborationRole = "owner" | "editor" | "viewer";

/** 可重试的协作批次。 */
export type CollaborationBatch = {
  /** 协作代数。 */
  epoch: CanvasVersion;
  /** 操作所依据的确认版本。 */
  baseRevision: CanvasVersion;
  /** 稳定重试键。 */
  opId: string;
  /** 同一事务确认的语义操作。 */
  changes: CollaborationChange[];
};

/** 服务端确认的提交或冲突详情。 */
export type CollaborationResult = {
  /** 协作代数。 */
  epoch: CanvasVersion;
  /** 已确认版本。 */
  revision: CanvasVersion;
  /** 接收者自己的操作 ID。 */
  opId: string;
  /** 已应用字段。 */
  changes?: CollaborationChange[];
  /** 需要用户处理的字段。 */
  conflicts?: Array<{
    kind: string;
    id: string;
    field: string;
    revision: CanvasVersion;
    value: unknown;
  }>;
  /** 个人撤销时因并发或引用限制保留的字段。 */
  skipped?: Array<{ kind: string; id: string; field: string; revision: CanvasVersion }>;
  /** 已处理的个人编辑版本。 */
  undoes?: CanvasVersion;
  redo?: boolean;
};

const path = (id: string) => `/canvas/${encodeURIComponent(id)}/collaboration`;

/** 打开授权快照并更新媒体访问票据。 */
export async function getCollaboration(id: string) {
  const result = await service.get<{
    epoch: CanvasVersion;
    role: CollaborationRole;
    canGenerate?: boolean;
    canCopyToLibrary?: boolean;
    canvas: BackendCanvasProjectDto;
  }>(path(id), undefined, { silent: true });
  return { ...result, canvas: mapCanvasProject(result.canvas) };
}

export function getCollaborationModePreview(id: string, active: boolean) {
  return service.get<CanvasModePreviewDto>(`${path(id)}/mode`, { active }, { silent: true });
}

/** 按已确认的影响范围和版本切换协作模式。 */
export function setCollaborationMode(
  id: string,
  revision: CanvasVersion,
  active: boolean,
  preview: string,
) {
  return service.put<{ active: boolean; epoch: CanvasVersion; revision: CanvasVersion }>(
    `${path(id)}/mode`,
    {
      revision,
      active,
      preview,
    },
    { silent: true },
  );
}

/** 按注册邮箱增加成员或调整授权。 */
export function setCollaborationMember(
  id: string,
  email: string,
  role: Exclude<CollaborationRole, "owner">,
) {
  return service.put<void>(`${path(id)}/member`, { email, role });
}

/** 独立调整当前编辑成员的生成授权。 */
export function setCanvasGenerationPermission(id: string, memberId: string, enabled: boolean) {
  return service.put<void>(`${path(id)}/member/${encodeURIComponent(memberId)}/generation`, {
    enabled,
  });
}

export function setCanvasLibraryPermission(id: string, memberId: string, enabled: boolean) {
  return service.put<void>(`${path(id)}/member/${encodeURIComponent(memberId)}/library`, {
    enabled,
  });
}

export function copyCanvasAssetToLibrary(
  id: string,
  epoch: CanvasVersion,
  nodeId: string,
  assetId: string,
  folderId: string | null,
  name?: string,
) {
  return service.post<void>(
    `${path(id)}/library`,
    { epoch, nodeId, asset_id: Number(assetId), folder_id: folderId, name },
    { timeout: 4 * 60_000 + 5_000 },
  );
}

/** 提交固定正文及操作 ID。 */
export function commitCollaboration(id: string, batch: CollaborationBatch) {
  return service.post<CollaborationResult>(`${path(id)}/ops`, batch, { silent: true });
}

/** 条件式撤销本人最近一次已确认编辑。 */
export function undoCollaboration(id: string, epoch: CanvasVersion, opId: string, redo = false) {
  return service.post<CollaborationResult>(
    `${path(id)}/undo`,
    { epoch, opId, ...(redo ? { redo } : {}) },
    { silent: true },
  );
}

/** 从最后确认的版本追赶，超过留存区间时由调用方读取新快照。 */
export function replayCollaboration(
  id: string,
  epoch: CanvasVersion,
  after: CanvasVersion,
  signal?: AbortSignal,
) {
  return service.get<{
    epoch: CanvasVersion;
    revision: CanvasVersion;
    cursor: CanvasVersion;
    title?: string;
    role: CollaborationRole;
    canGenerate?: boolean;
    canCopyToLibrary?: boolean;
    events: CollaborationResult[];
    snapshotRequired: boolean;
  }>(`${path(id)}/events`, { epoch, after }, { silent: true, ...(signal ? { signal } : {}) });
}

export type CanvasComment = {
  id: string;
  authorId: string;
  authorUsername?: string;
  body: string;
  mentions?: string[];
  createdAt: string;
};

export type CanvasCommentRequest = {
  epoch: CanvasVersion;
  nodeId: string;
  opId: string;
  body: string;
  mentions: string[];
};

export function getCanvasComments(
  id: string,
  nodeId: string,
  epoch: CanvasVersion,
  before?: string,
  signal?: AbortSignal,
) {
  return service.get<{
    comments: CanvasComment[];
    members: string[];
    usernames?: Record<string, string>;
    before?: string;
  }>(`${path(id)}/comments`, { nodeId, epoch, before }, { silent: true, signal });
}

export function addCanvasComment(id: string, request: CanvasCommentRequest, signal?: AbortSignal) {
  return service.post<CanvasComment>(`${path(id)}/comments`, request, { silent: true, signal });
}

export type CanvasMention = { id: string; nodeId: string };

export function getCanvasMentions(
  id: string,
  epoch: CanvasVersion,
  after?: string,
  signal?: AbortSignal,
) {
  return service.get<{ items: CanvasMention[] }>(
    `${path(id)}/mentions`,
    { epoch, after },
    { silent: true, signal },
  );
}

export type CanvasHistoryEntry = {
  revision: CanvasVersion;
  authorId?: string;
  authorUsername?: string;
  createdAt: string;
  targets: Array<{
    id: string;
    kind: "node" | "group" | "edge";
    action: "create" | "update" | "delete";
  }>;
};

export function getCanvasHistory(
  id: string,
  epoch: CanvasVersion,
  before?: CanvasVersion,
  signal?: AbortSignal,
) {
  return service.get<{ entries: CanvasHistoryEntry[]; before?: CanvasVersion }>(
    `${path(id)}/history`,
    { epoch, before },
    { silent: true, signal },
  );
}

/** 获取所有者可预览的指定确认版本。 */
export async function getCanvasHistoricalSnapshot(
  id: string,
  epoch: CanvasVersion,
  revision: CanvasVersion,
  signal?: AbortSignal,
) {
  const canvas = await service.get<BackendCanvasProjectDto>(
    `${path(id)}/history/${revision}`,
    { epoch },
    { silent: true, signal },
  );
  return mapCanvasProject(canvas);
}

/** 按稳定操作标识将历史版本另存为个人画布。 */
export function copyCanvasHistoricalSnapshot(
  id: string,
  epoch: CanvasVersion,
  revision: CanvasVersion,
  opId: string,
  signal?: AbortSignal,
) {
  return service.post<Pick<BackendCanvasProjectDto, "id">>(
    `${path(id)}/history/${revision}/copy`,
    { epoch, opId },
    { silent: true, signal },
  );
}
