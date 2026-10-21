import { describe, expect, test } from "bun:test";

import { ADMIN_NAV, findNav } from "@/pages/admin/admin-nav";

describe("后台导航", () => {
  test("“系统设置”分组下，存储配置之后依次是图片服务、注册设置、邮件服务、登录页展示、版本与升级", () => {
    const group = ADMIN_NAV.find((item) => item.label === "系统设置");
    expect(group?.items.map((item) => [item.to, item.label])).toEqual([
      ["settings/storage", "存储配置"],
      ["settings/image-processor", "图片服务"],
      ["settings/register", "注册设置"],
      ["settings/email", "邮件服务"],
      ["settings/showcase", "登录页展示"],
      ["settings/version", "版本与升级"],
    ]);
  });

  test("findNav 能认出存储配置页，面包屑显示所属分组与页面名", () => {
    const found = findNav("/admin/settings/storage");
    expect(found?.group.label).toBe("系统设置");
    expect(found?.item.label).toBe("存储配置");
  });

  test("findNav 能认出图片服务页", () => {
    expect(findNav("/admin/settings/image-processor")?.item.label).toBe("图片服务");
  });

  test("findNav 能认出版本与升级页", () => {
    expect(findNav("/admin/settings/version")?.item.label).toBe("版本与升级");
  });

  test("AI 配置分组：总览、任务队列、模型渠道、插件、技能、分析令牌；独立的“模型”页已并入渠道页，不再有单独的 Agent 分组", () => {
    const group = ADMIN_NAV.find((item) => item.label === "AI 配置");
    expect(group?.items.map((item) => [item.to, item.label])).toEqual([
      ["ai/overview", "总览"],
      ["ai/queue", "任务队列"],
      ["ai/channels", "模型渠道"],
      ["ai/plugins", "插件"],
      ["agent/skills", "技能"],
      ["agent/tokens", "分析令牌"],
    ]);
    expect(ADMIN_NAV.some((item) => item.label === "Agent")).toBe(false);
  });

  test("技能页的面包屑落在 AI 配置分组下，路径不变", () => {
    const found = findNav("/admin/agent/skills");
    expect(found?.group.label).toBe("AI 配置");
    expect(found?.item.label).toBe("技能");
  });

  test("分析令牌页的面包屑落在 AI 配置分组下", () => {
    const found = findNav("/admin/agent/tokens");
    expect(found?.group.label).toBe("AI 配置");
    expect(found?.item.label).toBe("分析令牌");
  });

  test("任务队列页的面包屑落在 AI 配置分组下", () => {
    const found = findNav("/admin/ai/queue");
    expect(found?.group.label).toBe("AI 配置");
    expect(found?.item.label).toBe("任务队列");
  });

  test("模型渠道页的面包屑", () => {
    const found = findNav("/admin/ai/channels");
    expect(found?.group.label).toBe("AI 配置");
    expect(found?.item.label).toBe("模型渠道");
    expect(findNav("/admin/ai/models")).toBeNull();
    expect(findNav("/admin/unknown")).toBeNull();
  });

  test("“用户”分组末尾是使用记录，“日志管理”分组只有操作审计", () => {
    const users = ADMIN_NAV.find((item) => item.label === "用户");
    expect(users?.items.map((item) => [item.to, item.label])).toEqual([
      ["users", "用户管理"],
      ["payment", "支付管理"],
      ["usage", "使用记录"],
    ]);
    const logs = ADMIN_NAV.find((item) => item.label === "日志管理");
    expect(logs?.items.map((item) => [item.to, item.label])).toEqual([["logs/audits", "操作审计"]]);
  });

  test("访问使用记录不会高亮用户管理，面包屑落在对应分组", () => {
    const usage = findNav("/admin/usage");
    expect(usage?.group.label).toBe("用户");
    expect(usage?.item.label).toBe("使用记录");
    expect(findNav("/admin/users")?.item.label).toBe("用户管理");
    const audits = findNav("/admin/logs/audits");
    expect(audits?.group.label).toBe("日志管理");
    expect(audits?.item.label).toBe("操作审计");
  });
});
