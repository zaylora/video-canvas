import { beforeEach, describe, expect, test } from "bun:test";

import type { BackendJobView, DramaJob } from "@/api/drama/type";
import { useDramaJobsStore } from "@/store/drama";
import { handleDramaJobEvent } from "@/utils/drama/job-events";
import { parseServerMessage } from "@/utils/ws/socket-client";

const job = (patch: Partial<DramaJob> = {}): DramaJob => ({
  id: "j1",
  step: "breakdown",
  episodes: [1],
  status: "running",
  progress: 10,
  credits: 0,
  errorCode: 0,
  errorMessage: "",
  createdAt: "2026-10-28T10:00:00Z",
  finishedAt: null,
  ...patch,
});

const rawJob = (patch: Partial<BackendJobView> = {}): BackendJobView => ({
  id: "j1",
  step: "breakdown",
  episodes: [1],
  status: "running",
  progress: 10,
  credits: 0,
  error_code: 0,
  error_message: "",
  created_at: "2026-10-28T10:00:00Z",
  finished_at: null,
  ...patch,
});

beforeEach(() => {
  useDramaJobsStore.setState({ byDrama: {}, settled: {}, watching: {} });
});

describe("短剧任务 store", () => {
  test("进度推送并进任务卡，不触发重新拉详情", () => {
    const { applyJob } = useDramaJobsStore.getState();
    applyJob("d1", job({ progress: 10 }));
    applyJob("d1", job({ progress: 50 }));
    const state = useDramaJobsStore.getState();
    expect(state.byDrama.d1).toHaveLength(1);
    expect(state.byDrama.d1[0].progress).toBe(50);
    expect(state.settled.d1).toBeUndefined();
  });

  test("任务收尾（成功 / 失败）触发一次重新拉详情，重复的终态推送不重复触发", () => {
    const { applyJob } = useDramaJobsStore.getState();
    applyJob("d1", job());
    applyJob("d1", job({ status: "succeeded", progress: 100, finishedAt: "x" }));
    expect(useDramaJobsStore.getState().settled.d1).toBe(1);
    applyJob("d1", job({ status: "succeeded", progress: 100, finishedAt: "x" }));
    expect(useDramaJobsStore.getState().settled.d1).toBe(1);
    applyJob("d1", job({ id: "j2", status: "failed", errorCode: 67008 }));
    expect(useDramaJobsStore.getState().settled.d1).toBe(2);
  });

  test("对账发现离线期间结束的任务：移出进行中并触发重新拉详情", () => {
    const { applyJob, applyActive } = useDramaJobsStore.getState();
    applyJob("d1", job({ id: "gone" }));
    applyActive("d1", []);
    const state = useDramaJobsStore.getState();
    expect(state.byDrama.d1).toEqual([]);
    expect(state.settled.d1).toBe(1);
  });

  test("只保留最近 5 条已结束的任务卡", () => {
    const { applyJob } = useDramaJobsStore.getState();
    for (let i = 1; i <= 8; i++) {
      applyJob(
        "d1",
        job({ id: `j${i}`, status: "succeeded", createdAt: `2026-10-28T10:0${i}:00Z` }),
      );
    }
    expect(useDramaJobsStore.getState().byDrama.d1).toHaveLength(5);
  });

  test("关掉任务卡只对已结束的有效", () => {
    const { applyJob, dismiss } = useDramaJobsStore.getState();
    applyJob("d1", job({ id: "run" }));
    applyJob("d1", job({ id: "bad", status: "failed" }));
    dismiss("d1", "run");
    dismiss("d1", "bad");
    expect(useDramaJobsStore.getState().byDrama.d1.map((j) => j.id)).toEqual(["run"]);
  });

  test("watch 按引用计数登记", () => {
    const stop1 = useDramaJobsStore.getState().watch("d1");
    const stop2 = useDramaJobsStore.getState().watch("d1");
    stop1();
    expect(useDramaJobsStore.getState().watching.d1).toBe(1);
    stop2();
    expect(useDramaJobsStore.getState().watching.d1).toBeUndefined();
  });
});

describe("WebSocket drama.job", () => {
  test("消息解析：合法的通过，缺字段的丢弃", () => {
    const ok = parseServerMessage(
      JSON.stringify({ type: "drama.job", data: { drama_id: "d1", job: rawJob() } }),
    );
    expect(ok?.type).toBe("drama.job");
    expect(
      parseServerMessage(JSON.stringify({ type: "drama.job", data: { drama_id: "d1" } })),
    ).toBeNull();
    expect(
      parseServerMessage(
        JSON.stringify({ type: "drama.job", data: { drama_id: 1, job: rawJob() } }),
      ),
    ).toBeNull();
  });

  test("事件处理：snake_case 的任务映射后并进对应短剧", () => {
    handleDramaJobEvent({ drama_id: "d9", job: rawJob({ progress: 40, episodes: null }) });
    const jobs = useDramaJobsStore.getState().byDrama.d9;
    expect(jobs[0]).toMatchObject({ id: "j1", progress: 40, episodes: [] });
  });
});
