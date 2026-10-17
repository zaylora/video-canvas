import { describe, expect, test } from "bun:test";

import {
  adminPageTitle,
  canvasPageTitle,
  formatPageTitle,
  homePageTitle,
} from "@/utils/page-title";

describe("浏览器标签页标题", () => {
  test("多段用 - 连接，末尾固定是应用名，空段被忽略", () => {
    expect(formatPageTitle("模型渠道", "后台管理")).toBe("模型渠道 - 后台管理 - 连镜");
    expect(formatPageTitle(undefined, "", "登录")).toBe("登录 - 连镜");
    expect(formatPageTitle()).toBe("连镜");
  });

  test("后台：当前页名称 + 后台管理；不认识的路径只显示后台管理", () => {
    expect(adminPageTitle("/admin/ai/channels")).toBe("模型渠道 - 后台管理 - 连镜");
    expect(adminPageTitle("/admin/settings/version")).toBe("版本与升级 - 后台管理 - 连镜");
    expect(adminPageTitle("/admin/whatever")).toBe("后台管理 - 连镜");
  });

  test("画布：显示画布名称，没起名时用「未命名画布」", () => {
    expect(canvasPageTitle("春日短片")).toBe("春日短片 - 连镜");
    expect(canvasPageTitle("  ")).toBe("未命名画布 - 连镜");
  });

  test("首页各页面", () => {
    expect(homePageTitle("/")).toBe("连镜");
    expect(homePageTitle("/canvases")).toBe("画布 - 连镜");
    expect(homePageTitle("/assets")).toBe("资产 - 连镜");
    expect(homePageTitle("/profile")).toBe("个人资料 - 连镜");
    expect(homePageTitle("/conversations/12")).toBe("对话 - 连镜");
  });
});
