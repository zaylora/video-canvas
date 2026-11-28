import { afterAll, beforeAll, describe, expect, test } from "bun:test";

import type { StorageHybridStatus } from "@/api/admin/storage/type.d";
import type { StorageHybridControl as ControlType } from "@/pages/admin/storage/storage-hybrid-control";
import { installDom, type DomEnv } from "@/tests/helpers/dom";

const off: StorageHybridStatus = {
  state: "off",
  cloud_id: 0,
  local_id: 0,
  limit_bytes: 0,
  error: "",
  has_cloud: true,
};

describe("存储配置混合策略", () => {
  let dom: DomEnv;
  let Control: typeof ControlType;

  beforeAll(async () => {
    dom = await installDom();
    Control = (await import("@/pages/admin/storage/storage-hybrid-control")).StorageHybridControl;
  });

  afterAll(() => dom?.restore());

  const button = (label: string) =>
    [...dom.win.document.querySelectorAll('[role="dialog"] button')].find(
      (element) => element.textContent?.trim() === label,
    );

  test("没有云端配置时不显示开关", async () => {
    const mounted = await dom.mount(
      <Control
        status={{ ...off, has_cloud: false }}
        canWrite
        saving={false}
        onChange={async () => {}}
      />,
    );
    expect(mounted.container.querySelector('[role="switch"]')).toBeNull();
    await mounted.unmount();
  });

  test("开启需确认，默认限额为零", async () => {
    const calls: unknown[] = [];
    const mounted = await dom.mount(
      <Control
        status={off}
        canWrite
        saving={false}
        onChange={async (...args) => {
          calls.push(args);
        }}
      />,
    );
    try {
      await dom.click(mounted.container.querySelector('[role="switch"]'));
      expect(dom.win.document.querySelector('[role="dialog"]')?.textContent).toContain(
        "开启混合策略",
      );
      expect(dom.win.document.querySelector<HTMLInputElement>('[role="dialog"] input')?.value).toBe(
        "0",
      );
      expect(calls).toEqual([]);
      await dom.click(button("确定"));
      expect(calls).toEqual([[true, 0]]);
    } finally {
      await mounted.unmount();
    }
  });

  test("关闭时须再次确认，取消保留启用状态", async () => {
    const calls: unknown[] = [];
    const mounted = await dom.mount(
      <Control
        status={{ ...off, state: "active", cloud_id: 2, local_id: 1 }}
        canWrite
        saving={false}
        onChange={async (...args) => {
          calls.push(args);
        }}
      />,
    );
    try {
      const toggle = mounted.container.querySelector('[role="switch"]');
      await dom.click(toggle);
      expect(dom.win.document.querySelector('[role="dialog"]')?.textContent).toContain(
        "关闭混合策略",
      );
      await dom.click(button("取消"));
      expect(toggle?.getAttribute("aria-checked")).toBe("true");
      expect(calls).toEqual([]);
      await dom.click(toggle);
      await dom.click(button("确定"));
      expect(calls).toEqual([[false, 0]]);
    } finally {
      await mounted.unmount();
    }
  });
});
