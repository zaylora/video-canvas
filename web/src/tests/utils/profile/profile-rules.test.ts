import { describe, expect, test } from "bun:test";

import { mapMe, mapStats } from "@/api/me";
import {
  avatarColor,
  avatarExportName,
  avatarUploadError,
  avatarInitial,
  displayName,
  joinedText,
  nicknameChanged,
  nicknameState,
  parseProfileTab,
  successRate,
  validateAvatarFile,
} from "@/utils/profile/profile-rules";

describe("昵称", () => {
  test("去掉首尾空白后最多 32 个字符，按字符而不是字节算", () => {
    expect(nicknameState("林".repeat(32), "lin").ok).toBe(true);
    expect(nicknameState(` ${"林".repeat(32)} `, "lin").ok).toBe(true);
    expect(nicknameState("a".repeat(33), "lin")).toEqual({
      ok: false,
      message: "最多 32 个字，当前 33",
    });
    /** emoji 按一个字符算 */
    expect(nicknameState("😀".repeat(32), "lin").ok).toBe(true);
  });
  test("不能包含换行和控制字符", () => {
    expect(nicknameState("林\n间", "lin")).toEqual({
      ok: false,
      message: "不能包含换行或控制字符",
    });
    expect(nicknameState("林\u0007间", "lin").ok).toBe(false);
    expect(nicknameState("林\u0085间", "lin").ok).toBe(false);
  });
  test("允许清空，提示会回落显示用户名；有内容时显示计数", () => {
    expect(nicknameState("   ", "lin_studio")).toEqual({
      ok: true,
      message: "留空时显示为 lin_studio",
    });
    expect(nicknameState("林间", "lin")).toEqual({ ok: true, message: "2 / 32" });
  });
  test("值有变化才可保存（首尾空白不算变化）", () => {
    expect(nicknameChanged("林间", "林间")).toBe(false);
    expect(nicknameChanged(" 林间 ", "林间")).toBe(false);
    expect(nicknameChanged("", "林间")).toBe(true);
    expect(nicknameChanged("林", "林间")).toBe(true);
  });
  test("显示名：昵称为空回落到用户名", () => {
    expect(displayName({ nickname: "林间", username: "lin" })).toBe("林间");
    expect(displayName({ nickname: "", username: "lin" })).toBe("lin");
    expect(displayName(null)).toBe("");
  });
});

describe("首字母头像", () => {
  test("取第一个字符并大写，emoji 不被劈开", () => {
    expect(avatarInitial("lin_studio")).toBe("L");
    expect(avatarInitial("林间")).toBe("林");
    expect(avatarInitial("😀abc")).toBe("😀");
    expect(avatarInitial("")).toBe("?");
  });
  test("底色按 user_id 固定，同一个 id 总是同一个颜色", () => {
    expect(avatarColor("42")).toBe(avatarColor("42"));
    expect(avatarColor("42")).not.toBe(avatarColor("43"));
    expect(avatarColor("42")).toMatch(/^oklch\(/);
  });
});

describe("头像文件校验", () => {
  const file = (type: string, size: number) => ({ type, size });
  test("只接受 PNG / JPEG / WebP / GIF", () => {
    for (const type of ["image/png", "image/jpeg", "image/webp", "image/gif"]) {
      expect(validateAvatarFile(file(type, 1024))).toBeNull();
    }
    expect(validateAvatarFile(file("text/plain", 10))).toBe(
      "不支持的格式（text/plain），请选择 PNG / JPEG / WebP / GIF",
    );
    expect(validateAvatarFile(file("", 10))).toBe(
      "不支持的格式（未知类型），请选择 PNG / JPEG / WebP / GIF",
    );
  });
  test("原图不超过 10MB", () => {
    expect(validateAvatarFile(file("image/png", 10 * 1024 * 1024))).toBeNull();
    expect(validateAvatarFile(file("image/jpeg", 12 * 1024 * 1024))).toBe("文件 12.0MB，超过 10MB");
  });
  test("导出文件名按实际编码选扩展名", () => {
    expect(avatarExportName("image/webp")).toBe("avatar.webp");
    expect(avatarExportName("image/png")).toBe("avatar.png");
  });
});

describe("身份卡与统计", () => {
  test("成功率：分母为 0 显示 null，其余保留一位小数", () => {
    expect(successRate({ success: 0, failed: 0 })).toBeNull();
    expect(successRate({ success: 93, failed: 7 })).toBe("93.0%");
    expect(successRate({ success: 2, failed: 1 })).toBe("66.7%");
  });
  test("加入时间按本地日期", () => {
    expect(joinedText("2025-03-12T08:00:00+08:00")).toMatch(/^2025-03-1\d 加入$/);
    expect(joinedText("")).toBe("");
  });
  test("tab 参数非法时回到概览", () => {
    expect(parseProfileTab("security")).toBe("security");
    expect(parseProfileTab("credits")).toBe("credits");
    expect(parseProfileTab("xxx")).toBe("overview");
    expect(parseProfileTab(null)).toBe("overview");
  });
});

describe("接口映射", () => {
  test("MeView：数字 id 转字符串，空头像转 null", () => {
    expect(
      mapMe({
        id: 42,
        username: "lin",
        nickname: "",
        email: "a@b.com",
        role: "user",
        avatar_url: "",
        created_at: "2025-03-12T00:00:00Z",
        email_verified_at: null,
      }),
    ).toEqual({
      id: "42",
      username: "lin",
      nickname: "",
      email: "a@b.com",
      role: "user",
      avatarUrl: null,
      createdAt: "2025-03-12T00:00:00Z",
      emailVerifiedAt: null,
    });
  });
  test("统计：snake_case 转 camelCase", () => {
    expect(
      mapStats({ total: 9, success: 7, failed: 1, last7d: 3, spent_credits: 120, canvas_count: 4 }),
    ).toEqual({ total: 9, success: 7, failed: 1, last7d: 3, spentCredits: 120, canvasCount: 4 });
  });
});

describe("头像上传失败的提示", () => {
  test("按错误码给出裁剪框内的就地提示", () => {
    expect(avatarUploadError(55005, "头像格式不支持，请上传 PNG / JPEG / WebP / GIF")).toBe(
      "头像格式不支持，请上传 PNG / JPEG / WebP / GIF",
    );
    expect(avatarUploadError(55006, "")).toBe("头像文件过大");
    expect(avatarUploadError(10000, "服务器内部错误")).toBe("头像服务暂不可用，请稍后重试");
    expect(avatarUploadError("NETWORK_ERROR", "网络异常，请检查后端服务")).toBe(
      "上传失败：网络异常，请检查后端服务",
    );
  });
});
