import { describe, expect, test } from "bun:test";

import { ADMIN_NAV, findNav } from "@/pages/admin/admin-nav";

describe("后台导航", () => {
  test("“系统设置”分组下，存储配置之后依次是图片服务、注册设置、邮件服务、登录页展示", () => {
    const group = ADMIN_NAV.find((item) => item.label === "系统设置");
    expect(group?.items.map((item) => [item.to, item.label])).toEqual([
      ["settings/storage", "存储配置"],
      ["settings/image-processor", "图片服务"],
      ["settings/register", "注册设置"],
      ["settings/email", "邮件服务"],
      ["settings/showcase", "登录页展示"],
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

  test("原有 AI 配置页不受影响", () => {
    expect(findNav("/admin/ai/channels")?.item.label).toBe("渠道");
    expect(findNav("/admin/unknown")).toBeNull();
  });
});
