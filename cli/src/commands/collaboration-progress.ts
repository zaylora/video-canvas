import type { CanvasNode, WireVersion } from "../types.ts";
import { compareVersions, isWireVersion } from "./collaboration-version.ts";
import type { CollaborationCommit } from "./collaboration.ts";

const sampleLimit = 256;
const expiry = 30_000;

type Sample = { runRevision: WireVersion; percent: number; expiresAt: number; emitted: boolean };

export class CanvasProgress {
  private readonly running = new Map<string, string>();
  private readonly samples = new Map<string, Sample>();
  private readonly canvasId: string;
  private readonly epoch: string;
  private revision: WireVersion;

  constructor(canvasId: string, epoch: WireVersion, revision: WireVersion, nodes: CanvasNode[]) {
    this.canvasId = canvasId;
    this.epoch = String(epoch);
    this.revision = revision;
    this.reset(revision, nodes);
  }

  reset(revision: WireVersion, nodes: CanvasNode[]) {
    this.revision = revision;
    this.running.clear();
    this.clearSamples();
    for (const { id, data } of nodes) {
      if (data?.status === "running" && isWireVersion(data.generationRevision))
        this.running.set(id, String(data.generationRevision));
    }
  }

  confirm(event: CollaborationCommit) {
    if (String(event.epoch) !== this.epoch || compareVersions(event.revision, this.revision) <= 0)
      return;
    this.revision = event.revision;
    for (const change of event.changes ?? []) {
      if (change.kind !== "node") continue;
      const data = change.value?.data as CanvasNode["data"];
      const status = data ? data.status : change.fields?.["data.status"];
      const runRevision = data
        ? data.generationRevision
        : change.fields?.["data.generationRevision"];
      if (
        change.action === "delete" ||
        (status !== undefined && status !== "running") ||
        runRevision === null
      )
        this.running.delete(change.id);
      else if (isWireVersion(runRevision) && (status === "running" || this.running.has(change.id)))
        this.running.set(change.id, String(runRevision));
    }
  }

  receive(value: unknown, now = performance.now()) {
    const notice = value as {
      canvasId?: unknown;
      epoch?: unknown;
      progress?: Partial<{ nodeId: string; runRevision: WireVersion; percent: number }>;
    } | null;
    if (
      notice?.canvasId !== this.canvasId ||
      !isWireVersion(notice.epoch) ||
      String(notice.epoch) !== this.epoch ||
      !notice.progress
    )
      return;
    const { nodeId, runRevision, percent } = notice.progress;
    if (
      typeof nodeId !== "string" ||
      nodeId.length === 0 ||
      nodeId.length > 256 ||
      !isWireVersion(runRevision) ||
      typeof percent !== "number" ||
      !Number.isInteger(percent) ||
      percent < 0 ||
      percent > 100
    )
      return;
    if (
      compareVersions(runRevision, this.revision) <= 0 &&
      this.running.get(nodeId) !== String(runRevision)
    )
      return;
    const previous = this.samples.get(nodeId);
    if (
      previous &&
      (compareVersions(runRevision, previous.runRevision) < 0 ||
        (String(runRevision) === String(previous.runRevision) && percent <= previous.percent))
    )
      return;
    if (!previous && this.samples.size >= sampleLimit) {
      for (const [id, sample] of this.samples) {
        if (sample.expiresAt <= now) this.samples.delete(id);
      }
      if (this.samples.size >= sampleLimit) return;
    }
    this.samples.set(nodeId, { runRevision, percent, emitted: false, expiresAt: now + expiry });
  }

  drain(now = performance.now()) {
    const output: Array<{ nodeId: string; runRevision: string; percent: number }> = [];
    for (const [nodeId, sample] of this.samples) {
      if (sample.expiresAt <= now) {
        this.samples.delete(nodeId);
        continue;
      }
      if (this.running.get(nodeId) !== String(sample.runRevision)) {
        if (compareVersions(sample.runRevision, this.revision) <= 0) this.samples.delete(nodeId);
        continue;
      }
      if (sample.emitted) continue;
      sample.emitted = true;
      output.push({ nodeId, runRevision: String(sample.runRevision), percent: sample.percent });
    }
    return output;
  }

  clearSamples() {
    this.samples.clear();
  }
}
