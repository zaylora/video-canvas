import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { TraceStep } from "@/api/admin/ai/type";
import type { TaskView } from "@/api/generation-task/type";
import { ResultPanel, type RunState, type TraceState } from "@/pages/admin/ai/result-panel";

const BUTTON = "复制修复信息";

const view = (over: Partial<TaskView> = {}): TaskView => ({
  id: 7,
  canvas_id: null,
  node_id: "",
  kind: "image",
  model_id: "yswg-gpt-image",
  status: "failed",
  progress: null,
  outputs: null,
  error_code: "plugin_error",
  error_message: "统一结果的 status 不合法",
  credits: 0,
  charged_credits: null,
  version: 3,
  deadline_at: null,
  created_at: "",
  submitted_at: null,
  finished_at: null,
  ...over,
});

const run = (over: Partial<TaskView> = {}): RunState => ({
  taskId: 7,
  view: view(over),
  timedOut: false,
});

const steps: TraceStep[] = [
  {
    name: "query",
    kind: "hook",
    hook: {
      name: "parseQueryResponse",
      input: { state: "SUCCESS" },
      output: { status: "SUCCESS" },
    },
    duration_ms: 3,
  },
];

const trace = (status: TraceState["status"], over: Partial<TraceState> = {}): TraceState => ({
  taskId: 7,
  status,
  steps: status === "ready" ? steps : [],
  ...over,
});

const render = (props: { run: RunState | null; trace: TraceState | null }) =>
  renderToStaticMarkup(
    <ResultPanel
      tab="run"
      onTabChange={() => {}}
      modelKey="yswg-gpt-image"
      dryRun={null}
      plugin={{ key: "yswg-image", version: "1.3.1" }}
      onRefreshTrace={() => {}}
      {...props}
    />,
  );

describe("结果面板：试跑失败时的“复制修复信息”", () => {
  test("失败且追踪已取到：失败提示里有按钮，原有的错误文案照常显示", () => {
    const out = render({ run: run(), trace: trace("ready") });
    expect(out).toContain("测试失败");
    expect(out).toContain("统一结果的 status 不合法");
    expect(out).toContain(BUTTON);
  });

  test("追踪取失败时也给：只带错误码和错误文案，没有失败步骤可带", () => {
    expect(render({ run: run(), trace: trace("error") })).toContain(BUTTON);
  });

  test("追踪还在拉取、还没开始拉取：先不给，免得复制出来漏掉失败步骤", () => {
    expect(render({ run: run(), trace: trace("loading") })).not.toContain(BUTTON);
    expect(render({ run: run(), trace: null })).not.toContain(BUTTON);
  });

  test("手上的追踪属于另一次试跑：不给", () => {
    expect(render({ run: run(), trace: trace("ready", { taskId: 6 }) })).not.toContain(BUTTON);
  });

  test("没有错误文案时仍显示兜底说明，按钮照给", () => {
    const out = render({
      run: run({ error_message: null, error_code: null }),
      trace: trace("ready"),
    });
    expect(out).toContain("上游没有返回原因");
    expect(out).toContain(BUTTON);
  });

  test("只有真正失败（failed）才给：过期、取消不是插件的问题，复制出去发给 AI 没有意义", () => {
    for (const status of ["expired", "canceled"] as const) {
      const out = render({ run: run({ status }), trace: trace("ready") });
      expect(out).toContain("测试失败"); // 失败提示照旧
      expect(out).not.toContain(BUTTON);
    }
  });

  test("成功或还在进行中：没有按钮", () => {
    expect(render({ run: run({ status: "succeeded" }), trace: trace("ready") })).not.toContain(
      BUTTON,
    );
    expect(render({ run: run({ status: "running" }), trace: trace("ready") })).not.toContain(
      BUTTON,
    );
    expect(render({ run: run({ status: "queued" }), trace: null })).not.toContain(BUTTON);
  });

  test("没有试跑记录：只有引导文字", () => {
    const out = render({ run: null, trace: null });
    expect(out).not.toContain(BUTTON);
    expect(out).toContain("开始测试");
  });
});

describe("结果面板：追踪标签里也有“复制修复信息”", () => {
  // 试跑结束后面板会自动切到“追踪”标签，用户落在这里，不该再让他切回“结果”去找按钮
  const traceTab = (props: { run: RunState | null; trace: TraceState | null }) =>
    renderToStaticMarkup(
      <ResultPanel
        tab="trace"
        onTabChange={() => {}}
        modelKey="yswg-gpt-image"
        dryRun={null}
        plugin={{ key: "yswg-image", version: "1.3.1" }}
        onRefreshTrace={() => {}}
        {...props}
      />,
    );

  test("试跑失败且追踪已取到：追踪头部有按钮，旁边还是原有的“复制全部（JSON）”", () => {
    const out = traceTab({ run: run(), trace: trace("ready") });
    expect(out).toContain(BUTTON);
    expect(out).toContain("复制全部（JSON）");
  });

  test("追踪还在拉取：不给（失败步骤还没到）", () => {
    expect(traceTab({ run: run(), trace: trace("loading") })).not.toContain(BUTTON);
  });

  test("试跑成功或进行中：追踪标签里没有按钮", () => {
    expect(traceTab({ run: run({ status: "succeeded" }), trace: trace("ready") })).not.toContain(
      BUTTON,
    );
    expect(traceTab({ run: run({ status: "running" }), trace: trace("ready") })).not.toContain(
      BUTTON,
    );
  });

  test("追踪取失败：只剩“没能取到追踪”的提示，没有按钮；结果标签里仍可复制错误信息", () => {
    expect(traceTab({ run: run(), trace: trace("error") })).not.toContain(BUTTON);
    expect(render({ run: run(), trace: trace("error") })).toContain(BUTTON);
  });

  test("追踪属于另一次试跑：不给", () => {
    expect(traceTab({ run: run(), trace: trace("ready", { taskId: 6 }) })).not.toContain(BUTTON);
  });
});
