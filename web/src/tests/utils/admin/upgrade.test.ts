import { describe, expect, test } from "bun:test";

import type { UpgradeInfo, UpgradeRunStatus } from "@/api/admin/system/type.d";
import {
  UPGRADE_STEPS,
  formatBytes,
  isOurRun,
  isRunFinished,
  manualUpgradeCommands,
  runProgress,
  upgradeBlockReason,
} from "@/utils/admin/upgrade";

const info = (patch: Partial<UpgradeInfo> = {}): UpgradeInfo => ({
  available: true,
  unavailable_reason: "",
  image_tag: "0.3.2",
  uses_latest: false,
  running: false,
  last: null,
  backups: [],
  log_tail: "",
  active_tasks: 0,
  rollback_target: "",
  current_migrations: [],
  ...patch,
});

const run = (patch: Partial<UpgradeRunStatus> = {}): UpgradeRunStatus => ({
  target: "0.4.0",
  previous: "0.3.2",
  phase: "pull",
  state: "running",
  message: "",
  backup: "",
  updated_at: "2026-10-20T08:00:05Z",
  ...patch,
});

describe("手动升级命令", () => {
  test("备份文件名带当前版本，.env 行和重建命令带目标版本", () => {
    const cmds = manualUpgradeCommands("0.3.2", "0.4.0");
    expect(cmds.map((c) => c.title)).toEqual([
      "备份数据库",
      "把部署目录 .env 里的版本改为 0.4.0",
      "拉取镜像并重建",
    ]);
    expect(cmds[0].command).toContain("pg_dump -U postgres -Fc video_canvas > backup-0.3.2-");
    expect(cmds[1].command).toBe("IMAGE_TAG=0.4.0");
    expect(cmds[2].command).toBe("docker compose pull && docker compose up -d");
  });
});

describe("升级进度", () => {
  test("按 update.sh 的阶段对应到界面的五步", () => {
    expect(UPGRADE_STEPS).toHaveLength(5);
    expect(runProgress(run({ phase: "prepare" })).step).toBe(0);
    expect(runProgress(run({ phase: "compose" })).step).toBe(0);
    expect(runProgress(run({ phase: "pull" })).step).toBe(1);
    expect(runProgress(run({ phase: "backup" })).step).toBe(2);
    expect(runProgress(run({ phase: "recreate" })).step).toBe(3);
    expect(runProgress(run({ phase: "health" })).step).toBe(4);
  });

  test("backend 重启的那几步标记为 restarting，前端据此显示重连提示", () => {
    expect(runProgress(run({ phase: "pull" })).restarting).toBe(false);
    expect(runProgress(run({ phase: "recreate" })).restarting).toBe(true);
    expect(runProgress(run({ phase: "health" })).restarting).toBe(true);
  });

  test("回退中停在最后一步并标记 rollingBack", () => {
    const p = runProgress(run({ phase: "rollback", state: "running" }));
    expect(p.rollingBack).toBe(true);
    expect(p.failedAt).toBe(4);
  });

  test("结局", () => {
    expect(runProgress(run({ phase: "finish", state: "succeeded" })).outcome).toBe("succeeded");
    expect(runProgress(run({ phase: "pull", state: "failed" })).outcome).toBe("failed");
    expect(runProgress(run({ phase: "pull", state: "failed" })).failedAt).toBe(1);
    expect(runProgress(run({ phase: "rollback", state: "failed_rolled_back" })).outcome).toBe(
      "failed_rolled_back",
    );
    expect(runProgress(run({ phase: "rollback", state: "failed_needs_attention" })).outcome).toBe(
      "failed_needs_attention",
    );
    expect(runProgress(run()).outcome).toBe("running");
  });

  test("未知阶段不崩，按准备处理", () => {
    expect(runProgress(run({ phase: "whatever" })).step).toBe(0);
  });
});

describe("这次运行的记录", () => {
  /** 发起前最近一次记录的时间：只和服务器写的时间比，不用浏览器时钟 */
  const baseline = "2026-10-20T08:00:00Z";

  test("没有记录、目标不同：不是这次的", () => {
    expect(isOurRun(null, "0.4.0", null)).toBe(false);
    expect(isOurRun(run({ target: "0.3.9" }), "0.4.0", null)).toBe(false);
  });

  test("发起前没有任何记录：目标对上就是这次的", () => {
    expect(isOurRun(run(), "0.4.0", null)).toBe(true);
  });

  test("记录还是发起前那条（同一目标的旧失败）：不是这次的", () => {
    const old = run({ state: "failed", updated_at: baseline });
    expect(isOurRun(old, "0.4.0", baseline)).toBe(false);
  });

  test("updater 写了新记录：是这次的，与浏览器时钟快慢无关", () => {
    expect(isOurRun(run({ updated_at: "2026-10-20T08:00:03Z" }), "0.4.0", baseline)).toBe(true);
  });
});

describe("这次运行是否已经结束", () => {
  const baseline = "2026-10-20T08:00:00Z";

  test("updater 还在跑：没结束", () => {
    expect(
      isRunFinished(
        info({ running: true, last: run({ state: "succeeded", phase: "finish" }) }),
        "0.4.0",
        baseline,
      ),
    ).toBe(false);
  });

  test("最近一次记录是发起前的旧运行：没结束", () => {
    const old = run({ state: "failed", updated_at: baseline });
    expect(isRunFinished(info({ last: old }), "0.4.0", baseline)).toBe(false);
  });

  test("目标版本不同：没结束", () => {
    expect(
      isRunFinished(
        info({ last: run({ target: "0.3.9", state: "succeeded" }) }),
        "0.4.0",
        baseline,
      ),
    ).toBe(false);
  });

  test("本次运行到了终态：结束", () => {
    const last = run({ state: "failed_rolled_back", updated_at: "2026-10-20T08:01:00Z" });
    expect(isRunFinished(info({ last }), "0.4.0", baseline)).toBe(true);
  });

  test("本次运行还是 running：没结束", () => {
    const last = run({ updated_at: "2026-10-20T08:01:00Z" });
    expect(isRunFinished(info({ last }), "0.4.0", baseline)).toBe(false);
  });
});

describe("能否一键升级", () => {
  test("可以", () => {
    expect(upgradeBlockReason(info(), true)).toBeNull();
  });

  test("各种不能的原因", () => {
    expect(upgradeBlockReason(info(), false)).toContain("超级管理员");
    expect(
      upgradeBlockReason(info({ available: false, unavailable_reason: "没有启用" }), true),
    ).toContain("没有启用");
    expect(upgradeBlockReason(info({ uses_latest: true }), true)).toContain("latest");
    expect(upgradeBlockReason(info({ running: true }), true)).toContain("进行中");
    expect(upgradeBlockReason(null, true)).not.toBeNull();
  });
});

describe("备份大小", () => {
  test("按 1024 进位，保留一位小数", () => {
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(165_696)).toBe("161.8 KB");
    expect(formatBytes(5 * 1024 * 1024)).toBe("5.0 MB");
  });
});
