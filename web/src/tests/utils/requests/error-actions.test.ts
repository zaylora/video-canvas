import { afterEach, describe, expect, test } from "bun:test";

import {
  clearErrorActions,
  getErrorAction,
  registerErrorAction,
} from "@/utils/requests/error-actions";

afterEach(() => clearErrorActions());

describe("错误提示的附带动作", () => {
  test("没注册过的错误码没有动作", () => {
    expect(getErrorAction(40001)).toBeNull();
  });

  test("注册后按错误码取到，点击时执行注册的回调", () => {
    let clicked = 0;
    registerErrorAction(40001, () => ({ label: "去充值", onClick: () => (clicked += 1) }));
    const action = getErrorAction(40001);
    expect(action?.label).toBe("去充值");
    action?.onClick();
    expect(clicked).toBe(1);
  });

  test("回调返回 null 表示当前不适用（如支付没开启），不显示按钮", () => {
    registerErrorAction(40001, () => null);
    expect(getErrorAction(40001)).toBeNull();
  });

  test("动作每次现取：注册时没有、之后才满足条件的动作也能生效", () => {
    let enabled = false;
    registerErrorAction(40001, () => (enabled ? { label: "去充值", onClick: () => {} } : null));
    expect(getErrorAction(40001)).toBeNull();
    enabled = true;
    expect(getErrorAction(40001)?.label).toBe("去充值");
  });

  test("同一个错误码重复注册，后一次覆盖前一次（模块热更新不会叠出两个按钮）", () => {
    registerErrorAction(40001, () => ({ label: "旧", onClick: () => {} }));
    registerErrorAction(40001, () => ({ label: "新", onClick: () => {} }));
    expect(getErrorAction(40001)?.label).toBe("新");
  });

  test("非数字的错误码（网络错误标识）安全地返回 null", () => {
    registerErrorAction(40001, () => ({ label: "x", onClick: () => {} }));
    expect(getErrorAction("NETWORK_ERROR")).toBeNull();
  });
});
