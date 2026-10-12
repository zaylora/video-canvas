import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { CheckResult } from "@/pages/admin/ai/channels/check-result";
import type { CheckState } from "@/pages/admin/ai/channels/use-channel-check";
import {
  CHECK_UNSUPPORTED_MESSAGE,
  classifyCheck,
  classifyCheckError,
} from "@/utils/admin/channel-check";

const BUTTON = "复制修复信息";

const done = (outcome: ReturnType<typeof classifyCheck>): CheckState => ({
  busy: false,
  outcome,
  fingerprint: "fp",
});

const render = (state: CheckState | undefined, inline = false) =>
  renderToStaticMarkup(
    <CheckResult state={state} inline={inline} plugin={{ key: "yswg-image", version: "1.3.1" }} />,
  );

describe("渠道检查结果：不通时的“复制修复信息”", () => {
  test("不通（failed）：结论、说明和按钮都在，结论仍是带 role=status 的状态区域", () => {
    const out = render(done(classifyCheck({ ok: false, message: "HTTP 401", duration_ms: 320 })));
    expect(out).toContain("不通");
    expect(out).toContain("HTTP 401 · 耗时 320ms");
    expect(out).toContain('role="status"');
    expect(out).toContain('data-check-kind="failed"');
    expect(out).toContain(BUTTON);
  });

  test("插件本身出错（钩子抛异常、返回非法请求）：这正是要发给 AI 修的，给按钮", () => {
    const out = render(
      done(
        classifyCheckError({
          status: 502,
          code: 50022,
          message: "插件钩子 buildCheckRequest 执行失败（js_error）：x is not a function",
        }),
      ),
    );
    expect(out).toContain("插件本身出错");
    expect(out).toContain('data-check-kind="plugin-fault"');
    expect(out).toContain(BUTTON);
  });

  test("插件出错但后端没给文案：没有东西可发，不给按钮", () => {
    expect(render(done(classifyCheckError({ status: 502, code: 50022 })))).not.toContain(BUTTON);
  });

  test("连通、不支持检查、没设 Key、运行器不可用、其他错误：都不给按钮", () => {
    const states = [
      done(classifyCheck({ ok: true, message: "HTTP 200", duration_ms: 20 })),
      done(classifyCheck({ ok: false, message: CHECK_UNSUPPORTED_MESSAGE, duration_ms: 0 })),
      done(classifyCheckError({ status: 409, code: 50015 })),
      done(classifyCheckError({ status: 503, code: 50021 })),
      done(classifyCheckError(new Error("x"))),
    ];
    for (const state of states) expect(render(state)).not.toContain(BUTTON);
  });

  test("检查中或没有结果：不给；表格里的一行小字（inline）也不给", () => {
    expect(render({ busy: true })).not.toContain(BUTTON);
    expect(render(undefined)).toBe("");
    const failed = done(classifyCheck({ ok: false, message: "HTTP 401", duration_ms: 1 }));
    expect(render(failed, true)).not.toContain(BUTTON);
  });
});
