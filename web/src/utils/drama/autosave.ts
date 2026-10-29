/** 自动保存的状态：已保存 / 有未保存的改动 / 保存中 / revision 冲突（停止自动保存）/ 保存失败（会重试） */
export type AutosaveStatus = "saved" | "dirty" | "saving" | "conflict" | "error";

export type AutosaverOptions = {
  /** 保存一版；revision 是当前已知版本号，返回保存后的新版本号。revision 冲突时抛错 */
  save: (value: string, revision: number) => Promise<number>;
  /** 判断一个错误是不是 revision 冲突 */
  isConflict: (error: unknown) => boolean;
  /** 起始版本号 */
  revision: number;
  /** 停手多久后保存（毫秒） */
  delay?: number;
  onStatus?: (status: AutosaveStatus) => void;
  /** 保存成功后通知新版本号 */
  onSaved?: (revision: number) => void;
};

/**
 * 带 revision 的自动保存（短剧剧本 / 大纲 / 人物共用）：
 * - 改动后停手 delay 毫秒才保存，保存串行，保存中又有新改动就接着再存一次；
 * - 用上一次保存返回的 revision 做乐观锁；撞上冲突不再重试，状态变 conflict，交给界面弹提示；
 * - 其他失败状态变 error，下次改动或 flush 会再试，内容不丢。
 */
export class Autosaver {
  private value: string | null = null;
  private saved: string | null = null;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private running: Promise<void> | null = null;
  private status: AutosaveStatus = "saved";
  private revision: number;
  private readonly delay: number;
  private readonly options: AutosaverOptions;

  constructor(options: AutosaverOptions) {
    this.options = options;
    this.revision = options.revision;
    this.delay = options.delay ?? 800;
  }

  getStatus(): AutosaveStatus {
    return this.status;
  }

  getRevision(): number {
    return this.revision;
  }

  /** 以当前内容为已保存基线（载入 / 重新载入最新后调用） */
  reset(value: string, revision: number) {
    this.clearTimer();
    this.value = value;
    this.saved = value;
    this.revision = revision;
    this.setStatus("saved");
  }

  /** 内容变了：和已保存的一样就当没改 */
  update(value: string) {
    this.value = value;
    if (this.status === "conflict") return;
    if (value === this.saved) {
      this.clearTimer();
      if (!this.running) this.setStatus("saved");
      return;
    }
    if (!this.running) this.setStatus("dirty");
    this.clearTimer();
    this.timer = setTimeout(() => void this.flush(), this.delay);
  }

  /** 立刻保存（失焦、切换文档、离开页面时调用）；等当前保存结束 */
  async flush(): Promise<void> {
    this.clearTimer();
    if (this.running) {
      await this.running;
      if (this.value !== this.saved && this.getStatus() !== "conflict") await this.flush();
      return;
    }
    if (this.status === "conflict" || this.value === null || this.value === this.saved) return;
    this.running = this.run();
    try {
      await this.running;
    } finally {
      this.running = null;
    }
    const after = this.getStatus();
    if (this.value !== this.saved && after !== "conflict" && after !== "error") {
      await this.flush();
    }
  }

  private async run() {
    const value = this.value as string;
    this.setStatus("saving");
    try {
      const next = await this.options.save(value, this.revision);
      this.revision = next;
      this.saved = value;
      this.options.onSaved?.(next);
      this.setStatus(this.value === value ? "saved" : "dirty");
    } catch (error) {
      this.setStatus(this.options.isConflict(error) ? "conflict" : "error");
    }
  }

  dispose() {
    this.clearTimer();
  }

  private clearTimer() {
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
  }

  private setStatus(status: AutosaveStatus) {
    if (this.status === status) return;
    this.status = status;
    this.options.onStatus?.(status);
  }
}
