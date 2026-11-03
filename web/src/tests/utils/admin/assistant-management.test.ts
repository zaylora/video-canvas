import { expect, test } from "bun:test";
import { adminTargetPath, targetPageLabel } from "@/utils/admin/assistant/change-result";
import { resolveRef } from "@/utils/agent/card";

test("新增管理对象的结果卡和展示卡跳转到现有后台页面", () => {
  for (const [target, label, path] of [
    ["user", "用户管理", "/admin/users"],
    ["register_settings", "注册设置", "/admin/settings/register"],
    ["smtp_settings", "邮件服务", "/admin/settings/email"],
    ["storage", "存储配置", "/admin/settings/storage"],
    ["image_processor", "图片服务", "/admin/settings/image-processor"],
  ]) {
    expect(targetPageLabel(target!)).toBe(label);
    expect(adminTargetPath(target!, "1")).toBe(path);
    expect(resolveRef({ type: target!, key: "1" }, "admin")).toEqual({ kind: "page", to: path });
    expect(resolveRef({ type: target!, key: "1" }, "canvas")).toBeNull();
  }
});
