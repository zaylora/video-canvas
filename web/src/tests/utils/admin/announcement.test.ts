import { describe, expect, test } from "bun:test";

import {
  emptyAnnouncementForm,
  formFromItem,
  formToBody,
  fromLocalInput,
  toLocalInput,
  validateAnnouncementForm,
  windowLabel,
} from "@/utils/admin/announcement";

describe("公告表单：时间", () => {
  test("本地时间输入框的值和 ISO 互转（按浏览器时区），空值是 null", () => {
    const local = "2026-11-12T22:30";
    const iso = fromLocalInput(local);
    expect(iso).toBe(new Date(2026, 10, 12, 22, 30).toISOString());
    expect(toLocalInput(iso)).toBe(local);
    expect(fromLocalInput("")).toBeNull();
    expect(toLocalInput(null)).toBe("");
    expect(fromLocalInput("not a date")).toBeNull();
  });

  test("列表里的时间范围：都没有是「不限」，缺一头写「立即」/「不限」", () => {
    const s = new Date(2026, 10, 12, 22, 0).toISOString();
    const e = new Date(2026, 10, 13, 0, 0).toISOString();
    expect(windowLabel(null, null)).toBe("不限");
    expect(windowLabel(s, e)).toBe("11-12 22:00 → 11-13 00:00");
    expect(windowLabel(null, e)).toBe("立即 → 11-13 00:00");
    expect(windowLabel(s, null)).toBe("11-12 22:00 → 不限");
  });
});

describe("公告表单：校验（与后端一致）", () => {
  const ok = { ...emptyAnnouncementForm(), title: "标题" };

  test("合法表单没有错误", () => {
    expect(validateAnnouncementForm(ok)).toEqual({});
  });

  test("标题按字符数算，去首尾空白后 1–60 个字", () => {
    expect(validateAnnouncementForm({ ...ok, title: "   " }).title).toBeDefined();
    expect(validateAnnouncementForm({ ...ok, title: "字".repeat(60) }).title).toBeUndefined();
    expect(validateAnnouncementForm({ ...ok, title: "字".repeat(61) }).title).toBeDefined();
  });

  test("结束时间要晚于开始时间", () => {
    expect(
      validateAnnouncementForm({ ...ok, startsAt: "2026-11-12T22:00", endsAt: "2026-11-12T22:00" })
        .endsAt,
    ).toBeDefined();
    expect(
      validateAnnouncementForm({ ...ok, startsAt: "2026-11-12T22:00", endsAt: "2026-11-12T23:00" })
        .endsAt,
    ).toBeUndefined();
  });
});

describe("公告表单：和接口互转", () => {
  test("提交时去首尾空白，空时间转 null", () => {
    expect(
      formToBody({
        title: " 标题 ",
        enabled: false,
        startsAt: "",
        endsAt: "2026-11-13T00:00",
      }),
    ).toEqual({
      title: "标题",
      enabled: false,
      starts_at: null,
      ends_at: new Date(2026, 10, 13, 0, 0).toISOString(),
    });
  });

  test("编辑时从后台条目还原表单", () => {
    const iso = new Date(2026, 10, 12, 22, 0).toISOString();
    expect(
      formFromItem({
        id: 1,
        title: "t",
        enabled: true,
        starts_at: iso,
        ends_at: null,
        sort: 0,
        status: "live",
        created_at: iso,
        updated_at: iso,
      }),
    ).toEqual({
      title: "t",
      enabled: true,
      startsAt: "2026-11-12T22:00",
      endsAt: "",
    });
  });

  test("新建表单默认启用、不限时", () => {
    expect(emptyAnnouncementForm()).toEqual({
      title: "",
      enabled: true,
      startsAt: "",
      endsAt: "",
    });
  });
});
