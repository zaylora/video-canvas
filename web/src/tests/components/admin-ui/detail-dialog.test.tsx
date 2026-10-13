import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import { DetailDialogBody } from "@/components/admin-ui/detail-dialog";

describe("详情弹窗内容区", () => {
  test("接口没返回：显示骨架并标记 aria-busy，不渲染真实内容", () => {
    const out = renderToStaticMarkup(
      <DetailDialogBody loading>
        <p>真实内容</p>
      </DetailDialogBody>,
    );
    expect(out).toContain('data-slot="skeleton"');
    expect(out).toContain('aria-busy="true"');
    expect(out).not.toContain("真实内容");
  });

  test("接口返回后：只渲染真实内容", () => {
    const out = renderToStaticMarkup(
      <DetailDialogBody loading={false}>
        <p>真实内容</p>
      </DetailDialogBody>,
    );
    expect(out).toContain("真实内容");
    expect(out).not.toContain('data-slot="skeleton"');
    expect(out).not.toContain("aria-busy");
  });

  test("可以换成自定义骨架", () => {
    const out = renderToStaticMarkup(
      <DetailDialogBody loading skeleton={<i>自定义骨架</i>}>
        <p>真实内容</p>
      </DetailDialogBody>,
    );
    expect(out).toContain("自定义骨架");
    expect(out).not.toContain('data-slot="skeleton"');
  });
});
