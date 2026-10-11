import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { FailedPlaceholder, PendingPlaceholder } from "@/components/generation-placeholder";

describe("PendingPlaceholder", () => {
  test("显示标题、说明，并标记为状态区域", () => {
    const out = renderToStaticMarkup(<PendingPlaceholder title="生成中" detail="已耗时 3秒" />);
    expect(out).toContain("生成中");
    expect(out).toContain("已耗时 3秒");
    expect(out).toContain('role="status"');
    expect(out).toContain('data-slot="skeleton"');
  });

  test("有进度才画百分比和进度条", () => {
    const withProgress = renderToStaticMarkup(<PendingPlaceholder title="生成中" progress={37} />);
    expect(withProgress).toContain("37%");
    expect(withProgress).toContain("width:37%");
    const without = renderToStaticMarkup(<PendingPlaceholder title="生成中" progress={null} />);
    expect(without).not.toContain("%");
  });

  test("给了 onCancel 才有取消按钮，取消中按钮禁用", () => {
    expect(renderToStaticMarkup(<PendingPlaceholder title="生成中" />)).not.toContain("取消");
    const out = renderToStaticMarkup(
      <PendingPlaceholder title="生成中" onCancel={() => {}} cancelling />,
    );
    expect(out).toContain("取消");
    expect(out).toContain("disabled");
  });

  test("尺寸由调用方的 className 和 style 决定", () => {
    const out = renderToStaticMarkup(
      <PendingPlaceholder
        title="生成中"
        className="h-48 rounded-xl"
        style={{ aspectRatio: 1.5 }}
      />,
    );
    expect(out).toContain("h-48 rounded-xl");
    expect(out).toContain("aspect-ratio:1.5");
  });
});

describe("FailedPlaceholder", () => {
  test("显示失败原因，标记为告警", () => {
    const out = renderToStaticMarkup(<FailedPlaceholder message="上游返回错误" />);
    expect(out).toContain("上游返回错误");
    expect(out).toContain('role="alert"');
  });

  test("退款说明和任务编号按需出现", () => {
    const plain = renderToStaticMarkup(<FailedPlaceholder message="失败" />);
    expect(plain).not.toContain("积分已退回");
    expect(plain).not.toContain("任务 ID");
    const full = renderToStaticMarkup(<FailedPlaceholder message="失败" refunded taskRef="fd36" />);
    expect(full).toContain("积分已退回");
    expect(full).toContain("fd36");
  });

  test("给了 onRetry 才有重试按钮，可禁用并带说明", () => {
    expect(renderToStaticMarkup(<FailedPlaceholder message="失败" />)).not.toContain("重试");
    const out = renderToStaticMarkup(
      <FailedPlaceholder message="失败" onRetry={() => {}} retryDisabled retryHint="模型已下线" />,
    );
    expect(out).toContain("重试");
    expect(out).toContain("disabled");
    expect(out).toContain('title="模型已下线"');
  });
});
