import { describe, expect, test } from "bun:test";

import { userIdFromToken } from "@/utils/storage/user-id";

/** 拼一个只有 payload 有意义的 JWT：签名不在前端校验 */
function tokenWith(payload: unknown) {
  const b64 = (value: unknown) => Buffer.from(JSON.stringify(value)).toString("base64url");
  return `${b64({ alg: "HS256", typ: "JWT" })}.${b64(payload)}.sig`;
}

describe("userIdFromToken", () => {
  test("取出令牌里的 user_id，转成字符串", () => {
    expect(userIdFromToken(tokenWith({ user_id: 42 }))).toBe("42");
  });

  test("没有 user_id：返回 null", () => {
    expect(userIdFromToken(tokenWith({ role: "user" }))).toBeNull();
  });

  test("user_id 不是正整数：返回 null，免得草稿键混进奇怪的值", () => {
    expect(userIdFromToken(tokenWith({ user_id: 0 }))).toBeNull();
    expect(userIdFromToken(tokenWith({ user_id: -3 }))).toBeNull();
    expect(userIdFromToken(tokenWith({ user_id: "42" }))).toBeNull();
    expect(userIdFromToken(tokenWith({ user_id: 1.5 }))).toBeNull();
  });

  test("不是合法 JWT：返回 null，不抛错", () => {
    expect(userIdFromToken("")).toBeNull();
    expect(userIdFromToken("abc")).toBeNull();
    expect(userIdFromToken("a.!!!.c")).toBeNull();
    expect(userIdFromToken(null)).toBeNull();
  });
});
