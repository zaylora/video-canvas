import {
  compareCanvasVersions,
  maxCanvasVersion,
  nextCanvasVersion,
  type CanvasVersion,
} from "@/utils/canvas/collaboration-version";

import type { CollaborationBatch, CollaborationResult } from "@/api/canvas/collaboration";
import type { CanvasGraphDto } from "@/api/canvas/type";
import {
  applyCollaborationChanges,
  collaborationFieldValue,
  collaborationPreviewChanges,
  diffCollaborationGraph,
  type CollaborationChange,
} from "./collaboration-graph";

type PendingEdit = {
  batch: CollaborationBatch;
  createdAt: number;
  dependencies: string[];
  sent: boolean;
  expired?: boolean;
  /** 服务端明确拒绝过该批次，等待成员处理。 */
  rejected?: boolean;
  origins?: Record<string, Record<string, unknown>>;
  conflict?: CollaborationResult["conflicts"];
};

/** 拖动起点和提交边界，在整个手势内保持固定。 */
export type CollaborationGesture = {
  revision: CanvasVersion;
  position: CanvasGraphDto["nodes"][number]["position"];
  active: boolean;
};

/** 粒度较长的编辑手势持有的字段起点。 */
export type CollaborationTextOrigin = {
  /** 待提交的字段。 */
  field: "data.prompt" | "data.text" | "data.scene";
  /** 开始时的确认版本。 */
  revision: CanvasVersion;
  /** 开始时显示的内容。 */
  value: unknown;
  /** 用户确认的内容。 */
  confirmed: unknown;
};

const draftLifetime = 24 * 60 * 60 * 1000;
const draftRetention = 7 * draftLifetime;

/** 草稿按当前标签页、账号、画布和协作代数隔离。 */
export function canvasCollaborationDraftKey(
  account: string | null,
  id: string,
  epoch: CanvasVersion,
) {
  return JSON.stringify(["canvas-collaboration", account, id, epoch]);
}

/** 只清理当前账号在同一画布上的失效草稿。 */
export function pruneCanvasCollaborationDrafts(
  account: string | null,
  id: string,
  keep?: CanvasVersion,
) {
  const prefix = `${JSON.stringify(["canvas-collaboration", account, id]).slice(0, -1)},`;
  const retained = keep === undefined ? null : canvasCollaborationDraftKey(account, id, keep);
  try {
    for (let index = sessionStorage.length - 1; index >= 0; index--) {
      const key = sessionStorage.key(index);
      if (key?.startsWith(prefix) && key !== retained) sessionStorage.removeItem(key);
    }
  } catch {
    return;
  }
}

/** Holds the acknowledged graph separately from local optimistic edits. */
export class CanvasCollaborationSession {
  readonly epoch: CanvasVersion;
  confirmed: CanvasGraphDto;
  revision: CanvasVersion;
  private pending: PendingEdit[] = [];
  private pendingUndo: { opId: string; createdAt: number; redo?: boolean } | null = null;
  private generationReceipts = new WeakMap<PendingEdit, CanvasVersion>();
  private localPreviews = new WeakMap<PendingEdit, CollaborationChange[]>();
  private optimisticCache: CanvasGraphDto | null = null;
  private optimisticCacheVersion = 0;
  private optimisticCacheBuiltVersion = -1;

  constructor(epoch: CanvasVersion, revision: CanvasVersion, graph: CanvasGraphDto) {
    this.epoch = epoch;
    this.revision = revision;
    this.confirmed = graph;
  }

  get optimistic() {
    if (this.optimisticCache && this.optimisticCacheBuiltVersion === this.optimisticCacheVersion)
      return this.optimisticCache;
    const paused = this.pausedIds();
    const graph = this.pending.reduce(
      (graph, edit) =>
        paused.has(edit.batch.opId)
          ? graph
          : applyCollaborationChanges(graph, this.localPreviews.get(edit) ?? edit.batch.changes),
      this.confirmed,
    );
    this.optimisticCache = graph;
    this.optimisticCacheBuiltVersion = this.optimisticCacheVersion;
    return graph;
  }

  private invalidateOptimistic() {
    this.optimisticCacheVersion++;
  }

  get conflicts() {
    const paused = this.pausedIds();
    return this.pending.filter((edit) => paused.has(edit.batch.opId));
  }

  get unsaved() {
    return this.pending.length > 0;
  }

  get undoId() {
    const undo = this.pendingUndo;
    if (!undo || Date.now() - undo.createdAt >= draftLifetime) return null;
    return undo.opId;
  }

  get redoPending() {
    return !!this.pendingUndo?.redo;
  }

  startUndo(redo = false) {
    this.pendingUndo = { opId: crypto.randomUUID(), createdAt: Date.now(), redo };
  }

  finishUndo() {
    this.pendingUndo = null;
  }

  restore(key: string) {
    try {
      const stored = sessionStorage.getItem(key);
      if (!stored) return;
      const draft = JSON.parse(stored) as {
        pending: PendingEdit[];
        undo?: { opId: string; createdAt: number; redo?: boolean };
      };
      if (!Array.isArray(draft.pending)) {
        sessionStorage.removeItem(key);
        return;
      }
      this.pending = draft.pending.filter((edit) => edit.batch.epoch === this.epoch);
      this.invalidateOptimistic();
      if (typeof draft.undo?.opId === "string" && Number.isFinite(draft.undo.createdAt)) {
        this.pendingUndo = draft.undo;
      }
      this.store(key);
    } catch {
      this.pending = [];
      this.finishUndo();
      this.invalidateOptimistic();
      this.store(key);
    }
  }

  store(key: string) {
    this.refreshDrafts();
    if (!this.undoId) this.finishUndo();
    try {
      if (this.pending.length === 0 && !this.pendingUndo) sessionStorage.removeItem(key);
      else
        sessionStorage.setItem(
          key,
          JSON.stringify({ pending: this.pending, undo: this.pendingUndo }),
        );
    } catch {
      return;
    }
  }

  private refreshDrafts() {
    const now = Date.now();
    const removed = new Set<string>();
    let changed = false;
    this.pending = this.pending.filter((edit) => {
      const age = now - edit.createdAt;
      if (
        !Number.isFinite(age) ||
        age >= draftRetention ||
        edit.dependencies.some((id) => removed.has(id))
      ) {
        removed.add(edit.batch.opId);
        changed = true;
        return false;
      }
      if (age >= draftLifetime && !edit.expired) {
        edit.expired = true;
        changed = true;
      }
      return true;
    });
    if (changed) this.invalidateOptimistic();
  }

  clear(key: string) {
    this.pending = [];
    this.finishUndo();
    this.invalidateOptimistic();
    this.store(key);
  }

  /** 清除未确认的生成请求，已确认的图和其他编辑不受影响。 */
  discardGeneration() {
    const previousLength = this.pending.length;
    this.pending = this.pending.filter(
      (edit) =>
        !edit.batch.changes.some((change) =>
          Object.hasOwn(change.fields ?? {}, "data.generationInput"),
        ),
    );
    if (this.pending.length !== previousLength) this.invalidateOptimistic();
  }

  confirmGeneration(id: string, model: string, input: Record<string, unknown>) {
    const edit: PendingEdit = {
      batch: {
        opId: crypto.randomUUID(),
        epoch: this.epoch,
        baseRevision: this.revision,
        changes: [{ kind: "node", id, fields: { "data.generationInput": { model, input } } }],
      },
      createdAt: Date.now(),
      dependencies: [],
      sent: false,
    };
    this.pending.push(edit);
    this.invalidateOptimistic();
    return () => this.generationReceipts.get(edit) ?? null;
  }

  capture(
    before: CanvasGraphDto,
    after: CanvasGraphDto,
    gestures?: Map<string, CollaborationGesture>,
    textOrigins?: Map<string, CollaborationTextOrigin>,
    baseRevision = this.revision,
  ): boolean {
    const changes = diffCollaborationGraph(before, after);
    if (changes.length === 0) return false;
    const previous = new Map(before.nodes.map((node) => [node.id, node]));
    const origins = Object.fromEntries(
      changes
        .filter((change) => change.fields)
        .map((change) => {
          const node = previous.get(change.id);
          const values: Record<string, unknown> = Object.fromEntries(
            Object.keys(change.fields ?? {}).map((field) => [
              field,
              node ? (collaborationFieldValue(node, field) ?? null) : null,
            ]),
          );
          const gesture = gestures?.get(change.id);
          if (gesture && !gesture.active && change.fields?.position) {
            change.fieldBases = {
              ...change.fieldBases,
              position: gesture.revision,
              ...(change.fields.parentId !== undefined ? { parentId: gesture.revision } : {}),
            };
            values.position = gesture.position;
          }
          const text = textOrigins?.get(change.id);
          if (
            text &&
            JSON.stringify(change.fields?.[text.field]) === JSON.stringify(text.confirmed)
          ) {
            change.fieldBases = { ...change.fieldBases, [text.field]: text.revision };
            values[text.field] = text.value;
            textOrigins?.delete(change.id);
          }
          return [change.id, values];
        }),
    );
    const dependencies = this.pending.filter((edit) =>
      edit.batch.changes.some((earlier) => changes.some((later) => dependsOn(later, earlier))),
    );
    const pending: PendingEdit = {
      batch: { opId: crypto.randomUUID(), epoch: this.epoch, baseRevision, changes },
      createdAt: Date.now(),
      dependencies: dependencies.map((edit) => edit.batch.opId),
      sent: false,
      origins,
    };
    this.localPreviews.set(
      pending,
      collaborationPreviewChanges(changes, new Map(after.nodes.map((node) => [node.id, node]))),
    );
    this.pending.push(pending);
    this.invalidateOptimistic();
    return true;
  }

  next() {
    this.refreshDrafts();
    const paused = this.pausedIds();
    const edit = this.pending.find(
      (entry) => !paused.has(entry.batch.opId) && entry.dependencies.length === 0,
    );
    if (!edit) return null;
    edit.sent = true;
    return edit.batch;
  }

  reject(opId: string) {
    const edit = this.pending.find((entry) => entry.batch.opId === opId);
    if (edit && !edit.rejected) {
      edit.rejected = true;
      this.invalidateOptimistic();
    }
  }

  apply(result: CollaborationResult, source: "receipt" | "replay" = "receipt") {
    if (
      result.epoch !== this.epoch ||
      compareCanvasVersions(result.revision, nextCanvasVersion(this.revision)) > 0
    )
      return false;
    if (result.conflicts?.length) {
      if (compareCanvasVersions(result.revision, this.revision) > 0) return false;
      const edit = this.pending.find((entry) => entry.batch.opId === result.opId);
      if (edit) {
        edit.conflict = result.conflicts;
        this.invalidateOptimistic();
      }
      return true;
    }
    if (
      result.revision === nextCanvasVersion(this.revision) &&
      (source === "replay" || result.changes?.length)
    ) {
      this.confirmed = applyCollaborationChanges(this.confirmed, result.changes ?? []);
      this.revision = result.revision;
      this.invalidateOptimistic();
    }
    if (compareCanvasVersions(result.revision, this.revision) <= 0 && result.opId)
      this.acknowledge(result);
    return compareCanvasVersions(result.revision, this.revision) <= 0;
  }

  replace(revision: CanvasVersion, graph: CanvasGraphDto) {
    if (compareCanvasVersions(revision, this.revision) < 0) return;
    this.revision = revision;
    this.confirmed = { ...graph, viewport: this.confirmed.viewport };
    this.invalidateOptimistic();
  }

  resolve(opId: string, applyDraft: boolean) {
    const paused = new Set([opId]);
    for (const edit of this.pending) {
      if (edit.dependencies.some((id) => paused.has(id))) paused.add(edit.batch.opId);
    }
    const removed = this.pending.filter((edit) => paused.has(edit.batch.opId));
    this.pending = this.pending.filter((edit) => !paused.has(edit.batch.opId));
    this.invalidateOptimistic();
    if (!applyDraft) return;
    const before = this.optimistic;
    const after = removed.reduce(
      (graph, edit) =>
        applyCollaborationChanges(
          graph,
          (this.localPreviews.get(edit) ?? edit.batch.changes).filter(
            (change) => !hasEquivalentConflictingEdge(graph, edit, change),
          ),
        ),
      before,
    );
    this.capture(before, after);
  }

  private acknowledge({ opId, revision, changes = [] }: CollaborationResult) {
    const accepted = this.pending.find((edit) => edit.batch.opId === opId);
    if (accepted?.batch.changes[0]?.fields?.["data.generationInput"])
      this.generationReceipts.set(accepted, revision);
    this.pending = this.pending.filter((edit) => edit.batch.opId !== opId);
    this.invalidateOptimistic();
    for (const edit of this.pending) {
      if (!edit.dependencies.includes(opId)) continue;
      edit.dependencies = edit.dependencies.filter((id) => id !== opId);
      if (edit.sent) continue;
      for (const pending of edit.batch.changes) {
        const confirmed = changes.find((change) => sameRecord(pending, change));
        if (!confirmed) continue;
        if (pending.action === "delete") {
          advanceDeleteBase(pending, confirmed, revision, edit.batch.baseRevision);
          continue;
        }
        if (!pending.fields) continue;
        for (const field of Object.keys(pending.fields)) {
          if (confirmed.action !== "create" && !Object.hasOwn(confirmed.fields ?? {}, field))
            continue;
          const bases = (pending.fieldBases ??= {});
          bases[field] = maxCanvasVersion(bases[field] ?? 0, revision);
        }
      }
    }
  }

  private pausedIds() {
    const paused = new Set<string>();
    for (const edit of this.pending) {
      if (
        edit.expired ||
        edit.conflict ||
        edit.rejected ||
        edit.dependencies.some((id) => paused.has(id))
      )
        paused.add(edit.batch.opId);
    }
    return paused;
  }
}

function hasEquivalentConflictingEdge(
  graph: CanvasGraphDto,
  edit: PendingEdit,
  change: CollaborationChange,
) {
  if (change.kind !== "edge" || change.action !== "create" || !change.value) return false;
  const value = change.value;
  return edit.conflict?.some(
    (conflict) =>
      conflict.kind === "edge" &&
      graph.edges.some(
        (edge) =>
          edge.id === conflict.id &&
          edge.source === value.source &&
          edge.target === value.target &&
          (edge.sourceHandle ?? null) === (value.sourceHandle ?? null) &&
          (edge.targetHandle ?? null) === (value.targetHandle ?? null),
      ),
  );
}

function advanceDeleteBase(
  pending: CollaborationChange,
  confirmed: CollaborationChange,
  revision: CanvasVersion,
  batchBase: CanvasVersion,
) {
  const base = maxCanvasVersion(batchBase, pending.recordBase ?? 0);
  const previous = confirmed.previousRevision;
  if (confirmed.action === "create" || (previous && compareCanvasVersions(previous, base) <= 0)) {
    pending.recordBase = maxCanvasVersion(base, revision);
  }
}

function sameRecord(first: CollaborationChange, second: CollaborationChange) {
  return first.kind === second.kind && first.id === second.id;
}

function dependsOn(later: CollaborationChange, earlier: CollaborationChange) {
  if (sameRecord(later, earlier)) {
    if (later.action || earlier.action) return true;
    return Object.keys(later.fields ?? {}).some((field) =>
      Object.hasOwn(earlier.fields ?? {}, field),
    );
  }
  if (earlier.kind === "edge" || !earlier.action) return false;
  const references = [
    later.value?.source,
    later.value?.target,
    later.value?.parentId,
    later.fields?.parentId,
  ];
  return references.includes(earlier.id);
}
