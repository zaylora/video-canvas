import type { CanvasGraphDto } from "@/api/canvas/type";

import { LOCAL_IDLE_MS, LOCAL_MAX_WAIT_MS, nextSaveDelay } from "./save-schedule";
import { displayStatus, statusSettleDelay, type SaveStatus } from "./save-status";

/** 云端保存失败后的自动重试间隔，用完了就停下，等用户手动点或联网事件 */
export const RETRY_DELAYS_MS = [5000, 15_000, 30_000];

type Draft = {
  /** 写草稿；返回 false 表示写入失败（配额、隐私模式等） */
  save: (baseVersion: number, graph: CanvasGraphDto) => Promise<boolean>;
  remove: () => Promise<void>;
};

type Deps = {
  initialVersion: number;
  /** 取当前完整图谱；还没准备好时返回 null */
  getGraph: () => CanvasGraphDto | null;
  saveCloud: (args: {
    baseVersion: number;
    graph: CanvasGraphDto;
    keepalive: boolean;
  }) => Promise<{ version: number }>;
  isConflict: (error: unknown) => boolean;
  /** 撞上冲突：拉回最新画布交给界面弹窗；返回 false 表示没拉到，当作普通失败 */
  onConflict: () => Promise<boolean>;
  /** 本地草稿；null 表示不可用（如认不出当前用户），退化为只走云端 */
  draft: Draft | null;
  onStatus: (status: SaveStatus) => void;
  now: () => number;
  setTimer: (fn: () => void, ms: number) => unknown;
  clearTimer: (handle: unknown) => void;
};

/**
 * 画布保存的两层调度，不依赖 React，时钟和定时器都可注入，方便测试。
 * - 本地草稿：停手 0.3 秒写 IndexedDB（最长 1 秒），几乎每次操作都落盘，不依赖网络
 * - 云端：停手 3 秒上传（最长 10 秒）；成功且没有新改动就删草稿
 * - 同一时刻最多一个在途云端请求；落地后若又有新改动，按窗口重新排期
 * - 云端失败自动重试 3 次；状态栏只在第二次失败后变红，冲突立即显示
 * - 云端上传本身不显示在状态栏上：内容已经在本地草稿里
 */
export class SaveCoordinator {
  private versionValue: number;
  private dirty = false;
  private firstDirtyAt = 0;
  private lastChangeAt = 0;
  private localDirty = false;
  private localFirstAt = 0;
  private localLastAt = 0;
  private lastLocalWriteAt: number | null = null;
  private draftOk: boolean;
  private flight: Promise<unknown> | null = null;
  private conflictFlag = false;
  private retry = 0;
  private failCount = 0;
  private active = false;
  private cloudTimer: unknown = null;
  private localTimer: unknown = null;
  private statusTimer: unknown = null;
  private lastStatus: SaveStatus | null = null;
  /** 草稿的写入和删除排成一队，保证后发的不会被先发的覆盖 */
  private writeTail: Promise<void> = Promise.resolve();
  /** 队里还有几个草稿操作没做完；为 0 时新操作同步发起，页面销毁前才来得及交出去 */
  private queued = 0;
  /** 队里还有几个「写入」没做完，写入期间状态栏不能判成已保存 */
  private writing = 0;

  private readonly deps: Deps;

  constructor(deps: Deps) {
    this.deps = deps;
    this.versionValue = deps.initialVersion;
    this.draftOk = deps.draft !== null;
  }

  get version() {
    return this.versionValue;
  }

  /** 改名等不经过图谱保存的请求成功后，把服务端返回的新 revision 接过来 */
  acceptVersion(version: number) {
    this.versionValue = version;
  }

  get conflicted() {
    return this.conflictFlag;
  }

  /** 云端还有没存上的内容：改动没发出去，或请求在途 */
  get hasUnsaved() {
    return this.dirty || this.flight !== null;
  }

  /** 云端有改动等着上传（不含在途请求） */
  get hasPendingCloud() {
    return this.dirty;
  }

  /** 组件挂载时 true，卸载时 false；不活跃时不再通知状态，也不再排本地写入 */
  setActive(active: boolean) {
    this.active = active;
    if (active) return;
    this.clearLocalTimer();
    this.clearStatusTimer();
  }

  /** 内容有改动：只标脏并排期，真正写草稿和发请求在各自的停手窗口之后 */
  changed() {
    const now = this.deps.now();
    if (!this.dirty) this.firstDirtyAt = now;
    this.dirty = true;
    this.lastChangeAt = now;
    this.markLocal(now);
    if (!this.conflictFlag) this.scheduleCloud();
    this.refreshStatus();
  }

  /**
   * 立即保存：先同步发起草稿写入（页面销毁前来得及交出去），再等在途请求落地后上传。
   * 返回保存后云端是否没有未保存内容。
   */
  async flush({ keepalive = false }: { keepalive?: boolean } = {}) {
    void this.writeLocal();
    while (this.flight) await this.flight.catch(() => undefined);
    this.retry = 0;
    return this.save(keepalive);
  }

  /** 独占一次云端请求：排在在途的后面，保证改名和图谱保存共用的 revision 不会撞车 */
  async exclusive<T>(task: () => Promise<T>): Promise<T> {
    while (this.flight) await this.flight.catch(() => undefined);
    const mine = task();
    this.flight = mine;
    try {
      return await mine;
    } finally {
      if (this.flight === mine) this.flight = null;
    }
  }

  /** 改名等操作结束后补排云端上传 */
  reschedule() {
    this.scheduleCloud();
  }

  /** 用户选了「加载最新」或「另存为」之后：清掉冲突态，本地未同步的内容不再提交 */
  dismissConflict() {
    this.conflictFlag = false;
    this.dirty = false;
    this.clearCloudTimer();
    this.refreshStatus();
  }

  // ---- 本地草稿 ----

  /** 排进草稿队：队里空着就同步开始，忙着就排在后面 */
  private enqueue(task: () => Promise<void>, visible: boolean): Promise<void> {
    const run = this.queued === 0 ? task() : this.writeTail.then(task);
    this.queued += 1;
    if (visible) this.writing += 1;
    this.writeTail = run
      .catch(() => undefined)
      .finally(() => {
        this.queued -= 1;
        if (visible) this.writing -= 1;
        this.refreshStatus();
      });
    return this.writeTail;
  }

  private markLocal(now: number) {
    if (!this.draftOk || !this.deps.draft) return;
    if (!this.localDirty) this.localFirstAt = now;
    this.localDirty = true;
    this.localLastAt = now;
    this.scheduleLocal();
  }

  private scheduleLocal() {
    this.clearLocalTimer();
    if (!this.localDirty || !this.active) return;
    const delay = nextSaveDelay({
      now: this.deps.now(),
      firstDirtyAt: this.localFirstAt,
      lastChangeAt: this.localLastAt,
      idleMs: LOCAL_IDLE_MS,
      maxWaitMs: LOCAL_MAX_WAIT_MS,
    });
    this.localTimer = this.deps.setTimer(() => void this.writeLocal(), delay);
  }

  /** 把当前图谱写进草稿；云端已经全部同步时改为删草稿 */
  private writeLocal(): Promise<void> {
    const draft = this.deps.draft;
    this.clearLocalTimer();
    if (!this.localDirty || !draft || !this.draftOk) return this.writeTail;

    if (!this.dirty && this.flight === null) {
      // 云端已经有了全部内容，草稿没有存在的意义
      this.localDirty = false;
      return this.enqueue(() => draft.remove(), false);
    }

    const graph = this.deps.getGraph();
    if (!graph) return this.writeTail;
    this.localDirty = false;
    const baseVersion = this.versionValue;
    const written = this.enqueue(async () => {
      const ok = await draft.save(baseVersion, graph).catch(() => false);
      if (!ok) this.draftOk = false;
      this.lastLocalWriteAt = this.deps.now();
    }, true);
    this.refreshStatus();
    return written;
  }

  // ---- 云端 ----

  private scheduleCloud() {
    this.clearCloudTimer();
    if (this.flight || this.conflictFlag || !this.dirty) return;
    const delay = nextSaveDelay({
      now: this.deps.now(),
      firstDirtyAt: this.firstDirtyAt,
      lastChangeAt: this.lastChangeAt,
    });
    this.cloudTimer = this.deps.setTimer(() => void this.save(), delay);
  }

  /** 调用前保证没有在途请求；返回「现在云端没有未保存的内容了」 */
  private async save(keepalive = false): Promise<boolean> {
    if (this.conflictFlag) return !this.dirty;
    if (!this.dirty) return true;
    const graph = this.deps.getGraph();
    if (!graph) return false;

    this.clearCloudTimer();
    // 这一批改动交给本次请求；请求期间新来的改动会重新标脏
    const batchStart = this.firstDirtyAt;
    this.dirty = false;
    this.refreshStatus();
    try {
      const saved = await this.exclusive(() =>
        this.deps.saveCloud({ baseVersion: this.versionValue, graph, keepalive }),
      );
      this.versionValue = saved.version;
      this.retry = 0;
      this.failCount = 0;
      if (this.dirty) {
        // 请求期间又有新改动：草稿以新版本为基准重写，云端按窗口补传
        this.markLocal(this.deps.now());
        void this.writeLocal();
        this.scheduleCloud();
        this.refreshStatus();
        return false;
      }
      this.removeDraftAfterSync();
      this.refreshStatus();
      return true;
    } catch (error) {
      // 内容没存上：放回脏态，下次保存发的是最新的全量
      this.firstDirtyAt = this.dirty ? Math.min(this.firstDirtyAt, batchStart) : batchStart;
      this.dirty = true;
      if (this.deps.isConflict(error) && (await this.deps.onConflict())) {
        this.conflictFlag = true;
        this.clearCloudTimer();
        this.refreshStatus();
        return false;
      }
      this.failCount += 1;
      this.refreshStatus();
      const delay = RETRY_DELAYS_MS[this.retry];
      if (delay !== undefined && !this.conflictFlag) {
        this.retry += 1;
        this.clearCloudTimer();
        this.cloudTimer = this.deps.setTimer(() => void this.save(), delay);
      }
      return false;
    }
  }

  /** 云端已同步：删掉草稿；本地还有没写的改动时由 writeLocal 在写入那一刻判断 */
  private removeDraftAfterSync() {
    const draft = this.deps.draft;
    if (!draft || !this.draftOk || this.localDirty) return;
    void this.enqueue(() => draft.remove(), false);
  }

  // ---- 状态 ----

  private refreshStatus() {
    if (!this.active) return;
    const now = this.deps.now();
    const localPending = this.localDirty || this.writing > 0;
    const status = displayStatus({
      conflict: this.conflictFlag,
      cloudFailCount: this.failCount,
      localPending,
      lastLocalWriteAt: this.lastLocalWriteAt,
      fallbackBusy: (!this.draftOk || !this.deps.draft) && (this.dirty || this.flight !== null),
      now,
    });
    if (status !== this.lastStatus) {
      this.lastStatus = status;
      this.deps.onStatus(status);
    }
    this.clearStatusTimer();
    const wait = statusSettleDelay({ now, lastLocalWriteAt: this.lastLocalWriteAt, localPending });
    if (wait > 0) this.statusTimer = this.deps.setTimer(() => this.refreshStatus(), wait);
  }

  private clearCloudTimer() {
    if (this.cloudTimer === null) return;
    this.deps.clearTimer(this.cloudTimer);
    this.cloudTimer = null;
  }

  private clearLocalTimer() {
    if (this.localTimer === null) return;
    this.deps.clearTimer(this.localTimer);
    this.localTimer = null;
  }

  private clearStatusTimer() {
    if (this.statusTimer === null) return;
    this.deps.clearTimer(this.statusTimer);
    this.statusTimer = null;
  }
}
