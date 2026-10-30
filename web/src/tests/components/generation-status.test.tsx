import { afterEach, describe, expect, spyOn, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { GenerationStatus, TaskGenerationStatus } from "@/components/generation-status";
import { NodeVideoBody } from "@/components/canvas/node-video-body";
import { useTasksStore } from "@/store/tasks";
import { deriveTaskStatus } from "@/utils/tasks/node-view";
import { makeTask } from "../utils/tasks/fixtures";

const NOW = Date.parse("2026-10-30T10:01:05Z");
const initialTasks = useTasksStore.getState().tasks;

/** 服务端静态渲染会读取 Zustand 初始快照，这里用当前快照模拟浏览器中的任务订阅。 */
const renderTask = (props: Parameters<typeof TaskGenerationStatus>[0]) => {
  const snapshot = useTasksStore.getInitialState();
  const tasks = snapshot.tasks;
  snapshot.tasks = useTasksStore.getState().tasks;
  try {
    return renderToStaticMarkup(<TaskGenerationStatus {...props} />);
  } finally {
    snapshot.tasks = tasks;
  }
};

afterEach(() => {
  useTasksStore.setState({ tasks: initialTasks });
});

describe("GenerationStatus：统一任务状态展示", () => {
  test("排队和转存与画布使用同一套文案，转存时不再提供取消", () => {
    const queued = renderToStaticMarkup(
      <GenerationStatus view={{ phase: "queued" }} onCancel={() => {}} />,
    );
    expect(queued).toContain("排队中");
    expect(queued).toContain("前面的任务处理完后会自动开始");
    expect(queued).toContain("取消");
    const saving = renderToStaticMarkup(
      <GenerationStatus view={{ phase: "finalizing" }} mediaType="video" onCancel={() => {}} />,
    );
    expect(saving).toContain("即将完成");
    expect(saving).toContain("正在保存视频");
    expect(saving).not.toContain("取消");
  });

  test("生成中展示耗时、百分比和进度条，框尺寸由调用方提供", () => {
    const view = { phase: "running" as const, elapsedMs: 65000, progress: 37 };
    const html = renderToStaticMarkup(
      <GenerationStatus view={view} className="size-full" style={{ aspectRatio: 9 / 16 }} />,
    );
    expect(html).toContain("已耗时 1分05秒");
    expect(html).toContain("37%");
    expect(html).toContain("width:37%");
    expect(html).toContain("aspect-ratio:0.5625");
    expect(html).toContain('role="status"');
    const canvas = renderToStaticMarkup(<NodeVideoBody view={view} placeholder="视频" />);
    expect(canvas).toContain("已耗时 1分05秒");
    expect(canvas).toContain("37%");
  });

  test("失败复用错误、退款和任务编号展示，重试按钮按需出现", () => {
    const html = renderToStaticMarkup(
      <GenerationStatus
        view={{ phase: "failed", message: "平台繁忙", refunded: true, taskRef: "abc" }}
        onRetry={() => {}}
      />,
    );
    expect(html).toContain("平台繁忙");
    expect(html).toContain("积分已退回");
    expect(html).toContain("abc");
    expect(html).toContain("重试");
    expect(html).toContain('role="alert"');
  });
});

describe("TaskGenerationStatus：按任务 ID 订阅最新快照", () => {
  test("未知快照显示同步中，不用创建时间冒充实际耗时", () => {
    const html = renderToStaticMarkup(
      <TaskGenerationStatus
        taskId="missing"
        fallback={{ status: "pending", error_message: null }}
      />,
    );
    expect(html).toContain("正在同步任务状态");
    expect(html).not.toContain("已耗时");
  });

  test("接收片段任务 ID 和摘要后，显示任务快照的实际进度与耗时", () => {
    const clock = spyOn(Date, "now").mockReturnValue(NOW);
    try {
      useTasksStore.setState({
        tasks: {
          "1": makeTask({ status: "running", progress: 37, submitted_at: "2026-10-30T10:00:00Z" }),
        },
      });
      const html = renderTask({
        taskId: "1",
        fallback: { status: "running", error_message: null },
      });
      expect(html).toContain("已耗时 1分05秒");
      expect(html).toContain("37%");
      expect(html).toContain("width:37%");
    } finally {
      clock.mockRestore();
    }
  });

  test("任务失败推送先到时，片场不再继续显示生成中", () => {
    useTasksStore.setState({
      tasks: { "1": makeTask({ status: "failed", error_message: "平台繁忙", charged_credits: 0 }) },
    });
    const html = renderTask({ taskId: "1", fallback: { status: "running", error_message: null } });
    expect(html).toContain("平台繁忙");
    expect(html).not.toContain("生成中");
  });

  test("短剧使用编码任务 ID 时也能订阅到十进制 ID 存储的快照", () => {
    const ref = "62385d8ed5d50315ea76e9ce6e6394cd";
    useTasksStore.setState({
      tasks: { "1": makeTask({ status: "running", progress: 42, task_ref: ref }) },
    });
    const html = renderTask({ taskId: ref, fallback: { status: "running", error_message: null } });
    expect(html).toContain("42%");
    expect(html).not.toContain("正在同步任务状态");
    expect(Object.keys(useTasksStore.getState().tasks)).toEqual(["1"]);
  });
});

describe("deriveTaskStatus：沿用公共阶段判断", () => {
  test("未提交、平台受理、转存和成功待回填使用统一阶段", () => {
    expect(deriveTaskStatus(makeTask({ status: "pending" }), NOW).phase).toBe("queued");
    expect(
      deriveTaskStatus(
        makeTask({ status: "queued", submitted_at: "2026-10-30T10:00:00Z", progress: 150 }),
        NOW,
      ),
    ).toEqual({ phase: "running", elapsedMs: 65000, progress: 100 });
    expect(deriveTaskStatus(makeTask({ status: "finalizing" }), NOW).phase).toBe("finalizing");
    expect(deriveTaskStatus(makeTask({ status: "succeeded" }), NOW).phase).toBe("finalizing");
  });

  test("失败、超时、取消不会被显示成即将完成，取消后按实际扣费显示退款", () => {
    for (const status of ["failed", "expired", "canceled"] as const) {
      const view = deriveTaskStatus(makeTask({ status, charged_credits: 5 }), NOW);
      expect(view.phase).toBe("failed");
      if (view.phase === "failed") expect(view.refunded).toBe(false);
    }
  });

  test("片段已收到终态时，不被任务库里的旧进行中快照覆盖", () => {
    const view = deriveTaskStatus(makeTask({ status: "running" }), NOW, {
      status: "failed",
      error_message: "平台繁忙",
    });
    expect(view.phase).toBe("failed");
    if (view.phase === "failed") expect(view.message).toBe("平台繁忙");
  });
});
