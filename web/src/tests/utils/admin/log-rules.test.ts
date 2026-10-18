import { describe, expect, test } from "bun:test";

import {
  actionView,
  AUDIT_MODULES,
  auditActorView,
  AUDIT_FILTER_KEYS,
  auditFiltersOf,
  auditRequest,
  auditSummary,
  auditTargetView,
  DEFAULT_USAGE_FILTERS,
  detailEntries,
  eventNote,
  formatDateTimeSec,
  hasUsageFilters,
  isTaskNotFound,
  outputCaption,
  failureHint,
  isActiveStatus,
  isTaskRef,
  ledgerRows,
  rangeWindow,
  readUserFilterState,
  timelineRows,
  outputRender,
  USAGE_FILTER_KEYS,
  usageFilterEntry,
  usageFiltersOf,
  usageRequest,
  usageSummaryRequest,
  usageStatusView,
  shouldPoll,
  TASK_POLL_MS,
} from "@/utils/admin/log-rules";
import { AUDIT_ACTION_LABEL } from "@/utils/admin/user-rules";

const NOW = new Date("2026-10-18T06:30:00.000Z");

describe("时间范围", () => {
  test("近 24 小时 / 7 天 / 30 天：to 是当前时刻，from 往前推整段", () => {
    const w = rangeWindow("7d", NOW);
    expect(w.to).toBe("2026-10-18T06:30:00.000Z");
    expect(w.from).toBe("2026-10-11T06:30:00.000Z");
    expect(rangeWindow("24h", NOW).from).toBe("2026-10-17T06:30:00.000Z");
    expect(rangeWindow("30d", NOW).from).toBe("2026-09-18T06:30:00.000Z");
  });

  test("今天：from 是本地零点", () => {
    const w = rangeWindow("today", NOW);
    const from = new Date(w.from);
    expect([from.getHours(), from.getMinutes(), from.getSeconds()]).toEqual([0, 0, 0]);
    expect(from.getTime()).toBeLessThanOrEqual(NOW.getTime());
  });
});

describe("使用记录：URL 参数 → 筛选状态", () => {
  test("什么都没有就是默认筛选（近 7 天）", () => {
    expect(usageFiltersOf({ q: "", filters: {} })).toEqual(DEFAULT_USAGE_FILTERS);
  });

  test("各参数读成筛选；非法值回退默认", () => {
    expect(
      usageFiltersOf({
        q: "alice",
        filters: {
          user_id: "12",
          status: "failed",
          kind: "video",
          model: "kling-v2",
          range: "30d",
          include_test: "true",
        },
      }),
    ).toEqual({
      q: "alice",
      userId: 12,
      status: "failed",
      kind: "video",
      model: "kling-v2",
      range: "30d",
      includeTest: true,
    });
    const bad = usageFiltersOf({
      q: "",
      filters: { user_id: "abc", status: "nope", kind: "x", range: "1y", include_test: "no" },
    });
    expect(bad).toEqual(DEFAULT_USAGE_FILTERS);
  });

  test("URL 认的筛选参数名", () => {
    expect([...USAGE_FILTER_KEYS].sort()).toEqual(
      ["include_test", "kind", "model", "range", "status", "user_id"].sort(),
    );
  });

  test("改一项筛选 → 写进 URL 的键值；默认值写空串（从 URL 删掉）", () => {
    expect(usageFilterEntry({ q: "bob" })).toEqual(["q", "bob"]);
    expect(usageFilterEntry({ status: "failed" })).toEqual(["status", "failed"]);
    expect(usageFilterEntry({ status: "all" })).toEqual(["status", ""]);
    expect(usageFilterEntry({ kind: "" })).toEqual(["kind", ""]);
    expect(usageFilterEntry({ model: "kling-v2" })).toEqual(["model", "kling-v2"]);
    expect(usageFilterEntry({ range: "30d" })).toEqual(["range", "30d"]);
    expect(usageFilterEntry({ range: "7d" })).toEqual(["range", ""]);
    expect(usageFilterEntry({ includeTest: true })).toEqual(["include_test", "true"]);
    expect(usageFilterEntry({ includeTest: false })).toEqual(["include_test", ""]);
    expect(usageFilterEntry({ userId: 12 })).toEqual(["user_id", "12"]);
    expect(usageFilterEntry({ userId: null })).toEqual(["user_id", ""]);
  });
});

describe("使用记录：列表查询 → 请求参数", () => {
  test("默认只带分页和近 7 天的起止时间", () => {
    expect(usageRequest({ page: 1, page_size: 20 }, NOW)).toEqual({
      page: 1,
      page_size: 20,
      from: "2026-10-11T06:30:00.000Z",
      to: "2026-10-18T06:30:00.000Z",
    });
  });

  test("各筛选项映射：range 换成 from/to，user_id 转数字，include_test 只在开启时传", () => {
    expect(
      usageRequest(
        {
          q: "alice",
          user_id: "12",
          status: "failed",
          kind: "video",
          model: "kling-v2",
          range: "30d",
          include_test: "true",
          page: 3,
          page_size: 50,
        },
        NOW,
      ),
    ).toEqual({
      q: "alice",
      user_id: 12,
      status: "failed",
      kind: "video",
      model: "kling-v2",
      include_test: "true",
      from: "2026-09-18T06:30:00.000Z",
      to: "2026-10-18T06:30:00.000Z",
      page: 3,
      page_size: 50,
    });
  });

  test("摘要请求：同一组筛选，不带分页", () => {
    const params = usageSummaryRequest({ q: " a ", filters: { status: "failed" } }, NOW);
    expect(params).toEqual({
      q: "a",
      status: "failed",
      from: "2026-10-11T06:30:00.000Z",
      to: "2026-10-18T06:30:00.000Z",
    });
    expect("page" in params).toBe(false);
  });

  test("32 位十六进制（任意大小写）识别为任务编号", () => {
    expect(isTaskRef("9f3ac1d7e2b84a600001" + "7be1c05d93a2")).toBe(true);
    expect(isTaskRef(" 9F3AC1D7E2B84A6000017BE1C05D93A2 ")).toBe(true);
    expect(isTaskRef("alice")).toBe(false);
    expect(isTaskRef("9f3ac1d7")).toBe(false);
  });
});

describe("操作审计：URL 与请求参数", () => {
  test("认的筛选参数名", () => {
    expect([...AUDIT_FILTER_KEYS].sort()).toEqual(["action", "module", "range"]);
  });

  test("URL → 筛选状态：q 是操作人，默认近 30 天，非法模块忽略", () => {
    expect(auditFiltersOf({ q: "", filters: {} })).toEqual({
      actor: "",
      module: "",
      range: "30d",
    });
    expect(auditFiltersOf({ q: "root", filters: { module: "channel", range: "7d" } })).toEqual({
      actor: "root",
      module: "channel",
      range: "7d",
    });
    expect(auditFiltersOf({ q: "", filters: { module: "bogus", range: "1y" } })).toEqual({
      actor: "",
      module: "",
      range: "30d",
    });
  });

  test("默认只带分页和近 30 天", () => {
    expect(auditRequest({ page: 1, page_size: 20 }, NOW)).toEqual({
      page: 1,
      page_size: 20,
      from: "2026-09-18T06:30:00.000Z",
      to: "2026-10-18T06:30:00.000Z",
    });
  });

  test("q 换成 actor；模块、动作原样带上", () => {
    expect(
      auditRequest(
        {
          q: "root",
          module: "channel",
          action: "channel.enable",
          range: "7d",
          page: 2,
          page_size: 20,
        },
        NOW,
      ),
    ).toEqual({
      actor: "root",
      module: "channel",
      action: "channel.enable",
      from: "2026-10-11T06:30:00.000Z",
      to: "2026-10-18T06:30:00.000Z",
      page: 2,
      page_size: 20,
    });
  });

  test("模块下拉的取值与后端约定一致", () => {
    expect(AUDIT_MODULES.map(([value]) => value)).toEqual([
      "",
      "user",
      "settings",
      "showcase",
      "system",
      "payment",
      "channel",
      "plugin",
      "model",
      "storage",
    ]);
  });
});

describe("产物怎么渲染", () => {
  test("media_type 不分大小写，带 mime 前缀只看斜杠前", () => {
    expect(outputRender({ media_type: "image", url: "/files/a.png" })).toEqual({
      kind: "image",
      hasUrl: true,
    });
    expect(outputRender({ media_type: "VIDEO", url: "/files/a.mp4" }).kind).toBe("video");
    expect(outputRender({ media_type: "video/mp4", url: "/files/a.mp4" }).kind).toBe("video");
    expect(outputRender({ media_type: " Audio/mpeg ", url: "/files/a.mp3" }).kind).toBe("audio");
    expect(outputRender({ media_type: "image/png", url: "/files/a.png" }).kind).toBe("image");
  });

  test("没有地址：照样识别类型，hasUrl 为 false", () => {
    expect(outputRender({ media_type: "video" })).toEqual({ kind: "video", hasUrl: false });
    expect(outputRender({ media_type: "image", url: "" })).toEqual({
      kind: "image",
      hasUrl: false,
    });
  });

  test("文本：media_type 是 text，或没有已知类型但带了正文", () => {
    expect(outputRender({ media_type: "text", text: "你好" })).toEqual({
      kind: "text",
      hasUrl: false,
    });
    expect(outputRender({ media_type: "text/plain", text: "hi" }).kind).toBe("text");
    expect(outputRender({ media_type: "", text: "hi" }).kind).toBe("text");
  });

  test("未知类型：当作普通文件", () => {
    expect(outputRender({ media_type: "application/zip", url: "/files/a.zip" })).toEqual({
      kind: "file",
      hasUrl: true,
    });
    expect(outputRender({ media_type: "" })).toEqual({ kind: "file", hasUrl: false });
  });
});

describe("任务状态", () => {
  test("八种状态都有文案；进行中的四种标记为 active", () => {
    expect(usageStatusView("pending")).toMatchObject({ label: "排队中", active: true });
    expect(usageStatusView("queued")).toMatchObject({ label: "平台排队", active: true });
    expect(usageStatusView("running")).toMatchObject({ label: "生成中", active: true });
    expect(usageStatusView("finalizing")).toMatchObject({ label: "转存中", active: true });
    expect(usageStatusView("succeeded")).toMatchObject({ label: "成功", tone: "success" });
    expect(usageStatusView("failed")).toMatchObject({ label: "失败", tone: "danger" });
    expect(usageStatusView("canceled")).toMatchObject({ label: "已取消", tone: "neutral" });
    expect(usageStatusView("expired")).toMatchObject({ label: "已超时", tone: "warning" });
  });

  test("未知状态原样显示、不当作进行中", () => {
    expect(usageStatusView("weird")).toMatchObject({ label: "weird", active: false });
    expect(isActiveStatus("weird")).toBe(false);
    expect(isActiveStatus("running")).toBe(true);
    expect(isActiveStatus("failed")).toBe(false);
  });
});

describe("失败提示（老任务降级）", () => {
  test("成功 / 进行中的任务没有提示", () => {
    expect(failureHint({ status: "succeeded", upstream: null, hasEvents: true })).toBeNull();
    expect(failureHint({ status: "running", upstream: null, hasEvents: true })).toBeNull();
  });

  test("有上游错误原文就不提示", () => {
    expect(failureHint({ status: "failed", upstream: "HTTP 503", hasEvents: true })).toBeNull();
  });

  test("老任务（没有事件）：创建于日志功能上线前", () => {
    for (const status of ["failed", "expired", "canceled"]) {
      expect(failureHint({ status, upstream: null, hasEvents: false })).toBe(
        "此任务创建于日志功能上线前，没有上游错误记录。",
      );
    }
  });

  test("新任务超时 / 取消：说明这类失败本来没有上游错误", () => {
    expect(failureHint({ status: "expired", upstream: null, hasEvents: true })).toBe(
      "这类失败（排队超时）没有上游错误。",
    );
    expect(failureHint({ status: "canceled", upstream: null, hasEvents: true })).toBe(
      "这类失败（用户取消）没有上游错误。",
    );
  });

  test("新任务失败但没有原文", () => {
    expect(failureHint({ status: "failed", upstream: null, hasEvents: true })).toBe(
      "这次失败没有记录到上游错误原文。",
    );
  });
});

describe("状态时间线", () => {
  const events = [
    {
      id: 1,
      event: "created",
      from_status: "",
      to_status: "pending",
      detail: null,
      created_at: "2026-10-18T06:00:00Z",
    },
    {
      id: 2,
      event: "submitted",
      from_status: "pending",
      to_status: "running",
      detail: { provider_task_id_set: true },
      created_at: "2026-10-18T06:00:30Z",
    },
    {
      id: 3,
      event: "failed",
      from_status: "running",
      to_status: "failed",
      detail: { class: "terminal", code: "content_rejected", upstream: "very long", reason: "x" },
      created_at: "2026-10-18T06:02:30Z",
    },
  ];

  test("按事件 id 升序，给出文案、迁移与距上一步耗时", () => {
    const rows = timelineRows([...events].reverse());
    expect(rows.map((r) => r.label)).toEqual(["提交了任务", "平台开始处理", "失败了"]);
    expect(rows.map((r) => r.transition)).toEqual(["排队中", "排队中 → 生成中", "生成中 → 失败"]);
    expect(rows.map((r) => r.deltaMs)).toEqual([null, 30_000, 120_000]);
    expect(rows[2].tone).toBe("danger");
  });

  test("空事件得到空数组（老任务）", () => {
    expect(timelineRows([])).toEqual([]);
  });

  test("备注：提炼已知键、跳过超长的 upstream、未知键原样显示", () => {
    expect(eventNote(null)).toBe("");
    expect(eventNote({ provider_task_id_set: true })).toBe("已记录上游任务号");
    expect(
      eventNote({ class: "terminal", code: "a", upstream: "long", reason: "user_cancel" }),
    ).toBe("class=terminal · code=a · 用户取消");
    expect(eventNote({ foo: 1, bar: { x: 1 } })).toBe('foo=1 · bar={"x":1}');
  });

  test("取消原因：用户取消、管理员取消（后台封禁时强制取消）、超时，未知原因原样显示", () => {
    expect(eventNote({ reason: "user_cancel" })).toBe("用户取消");
    expect(eventNote({ reason: "admin_cancel" })).toBe("管理员取消");
    expect(eventNote({ reason: "deadline" })).toBe("超过截止时间");
    expect(eventNote({ reason: "something_new" })).toBe("reason=something_new");
  });
});

describe("积分流水", () => {
  test("类型中文名，金额带符号", () => {
    const rows = ledgerRows([
      { id: 1, type: "freeze", amount: -120, created_at: "2026-10-18T06:00:00Z" },
      { id: 2, type: "refund", amount: 120, created_at: "2026-10-18T06:02:00Z" },
      { id: 3, type: "other", amount: 0, created_at: "2026-10-18T06:02:00Z" },
    ]);
    expect(rows.map((r) => [r.label, r.amountText])).toEqual([
      ["冻结", "-120"],
      ["退款", "+120"],
      ["other", "0"],
    ]);
  });
});

describe("动作中文名", () => {
  // 后端 model/admin_audit.go 与 model/ai_audit.go 的全部动作常量
  const BACKEND_ACTIONS = [
    "user.credit_adjust",
    "user.ban",
    "user.unban",
    "user.role",
    "user.reset_password",
    "user.limit",
    "settings.register",
    "settings.smtp",
    "showcase.create",
    "showcase.update",
    "showcase.delete",
    "showcase.order",
    "showcase.settings",
    "system.upgrade",
    "system.rollback",
    "payment.settings_update",
    "payment.channel_update",
    "payment.channel_enable",
    "payment.channel_disable",
    "payment.product_create",
    "payment.product_update",
    "payment.product_delete",
    "payment.product_sort",
    "payment.order_query",
    "payment.order_close",
    "plugin.upload",
    "plugin.enable",
    "plugin.delete",
    "channel.create",
    "channel.update",
    "channel.secret",
    "channel.trusted",
    "channel.credential",
    "channel.secret_key",
    "channel.sort",
    "model.import",
    "model.protocol",
    "plugin.upgrade_models",
    "channel.enable",
    "channel.auto_off",
    "channel.delete",
    "storage.create",
    "storage.update",
    "storage.secret",
    "storage.default",
    "storage.delete",
  ];

  test("覆盖后端全部动作：都有中文名，且不是原样的 action", () => {
    for (const action of BACKEND_ACTIONS) {
      const view = actionView(action);
      expect(view.known).toBe(true);
      expect(view.label).not.toBe(action);
    }
  });

  test("只有 channel.trusted 与 channel.credential 是历史动作", () => {
    const legacy = BACKEND_ACTIONS.filter((a) => actionView(a).legacy);
    expect(legacy).toEqual(["channel.trusted", "channel.credential"]);
  });

  test("沿用用户管理里已有的动作名，不重复定义", () => {
    for (const [action, label] of Object.entries(AUDIT_ACTION_LABEL)) {
      expect(actionView(action).label).toBe(label);
    }
  });

  test("未知动作原样显示", () => {
    expect(actionView("foo.bar")).toEqual({ label: "foo.bar", legacy: false, known: false });
  });
});

describe("操作人与对象", () => {
  test("actor_id=0 是系统（自动）", () => {
    expect(auditActorView(0, "")).toEqual({ name: "系统", auto: true });
    expect(auditActorView(3, "root")).toEqual({ name: "root", auto: false });
    expect(auditActorView(9, "")).toEqual({ name: "#9", auto: false });
  });

  test("对象：类型中文名 + 名称，没有名称用编号", () => {
    expect(
      auditTargetView({ target_type: "user", target_ref: "12", target_name: "alice" }),
    ).toEqual({
      type: "用户",
      name: "alice",
    });
    expect(
      auditTargetView({ target_type: "channel", target_ref: "kling", target_name: "" }),
    ).toEqual({
      type: "渠道",
      name: "kling",
    });
    expect(auditTargetView({ target_type: "weird", target_ref: "", target_name: "" })).toEqual({
      type: "weird",
      name: "—",
    });
  });
});

describe("审计摘要提炼", () => {
  test("调整积分：积分变化 + 备注", () => {
    expect(auditSummary("user.credit_adjust", { delta: 100, note: "补偿" })).toBe(
      "积分 +100，备注：补偿",
    );
    expect(auditSummary("user.credit_adjust", { delta: -5, note: "" })).toBe("积分 -5");
  });

  test("并发上限 / 角色 / 升级：前后值", () => {
    expect(auditSummary("user.limit", { from: null, to: 8 })).toBe("并发上限 全局默认 → 8");
    expect(auditSummary("user.role", { from: "user", to: "admin" })).toBe("角色 user → admin");
    expect(auditSummary("system.upgrade", { from: "0.4.1", to: "0.4.2" })).toBe("0.4.1 → 0.4.2");
  });

  test("渠道 / 支付渠道启停", () => {
    expect(auditSummary("channel.enable", { from: false, to: true })).toBe("已停用 → 已启用");
  });

  test("插件上传：版本", () => {
    expect(auditSummary("plugin.upload", { version: "1.2.0", sha256: "abc" })).toBe("版本 1.2.0");
  });

  test("提炼不出时退回 key=value 拼接；detail 为空显示破折号", () => {
    expect(auditSummary("channel.update", { fields: ["name"], n: 2 })).toBe('fields=["name"]，n=2');
    expect(auditSummary("channel.update", null)).toBe("—");
    expect(auditSummary("channel.update", {})).toBe("—");
  });

  test("detail 为 JSON 字符串也能解析", () => {
    expect(auditSummary("user.credit_adjust", '{"delta":10,"note":"a"}')).toBe("积分 +10，备注：a");
  });
});

describe("从用户管理进入", () => {
  test("读取 location.state 里的 userId", () => {
    expect(readUserFilterState({ userId: 12 })).toBe(12);
  });
  test("没有或不合法时为 null", () => {
    expect(readUserFilterState(null)).toBeNull();
    expect(readUserFilterState(undefined)).toBeNull();
    expect(readUserFilterState({})).toBeNull();
    expect(readUserFilterState({ userId: "12" })).toBeNull();
    expect(readUserFilterState({ userId: 0 })).toBeNull();
  });
});

describe("进行中任务的轮询", () => {
  test("进行中且页面可见才刷新，间隔 5 秒", () => {
    expect(TASK_POLL_MS).toBe(5000);
    expect(shouldPoll("running", true)).toBe(true);
    expect(shouldPoll("running", false)).toBe(false);
    expect(shouldPoll("succeeded", true)).toBe(false);
  });
});

describe("任务不存在", () => {
  test("HTTP 404 或业务码 40004 都算找不到", () => {
    expect(isTaskNotFound({ status: 404, code: 40004 })).toBe(true);
    expect(isTaskNotFound({ status: 200, code: 40004 })).toBe(true);
    expect(isTaskNotFound({ status: 404 })).toBe(true);
    expect(isTaskNotFound({ status: 500, code: 50000 })).toBe(false);
    expect(isTaskNotFound(null)).toBe(false);
    expect(isTaskNotFound(new Error("x"))).toBe(false);
  });
});

describe("是否有生效的筛选", () => {
  test("默认筛选没有；任一项变化都算有", () => {
    expect(hasUsageFilters(DEFAULT_USAGE_FILTERS)).toBe(false);
    // 时间范围单独放宽按钮处理，不算“筛选”
    expect(hasUsageFilters({ ...DEFAULT_USAGE_FILTERS, range: "30d" })).toBe(false);
    expect(hasUsageFilters({ ...DEFAULT_USAGE_FILTERS, q: "a" })).toBe(true);
    expect(hasUsageFilters({ ...DEFAULT_USAGE_FILTERS, userId: 1 })).toBe(true);
    expect(hasUsageFilters({ ...DEFAULT_USAGE_FILTERS, status: "failed" })).toBe(true);
    expect(hasUsageFilters({ ...DEFAULT_USAGE_FILTERS, kind: "video" })).toBe(true);
    expect(hasUsageFilters({ ...DEFAULT_USAGE_FILTERS, model: "m" })).toBe(true);
    expect(hasUsageFilters({ ...DEFAULT_USAGE_FILTERS, includeTest: true })).toBe(true);
  });
});

describe("产物说明与明细展示", () => {
  test("产物说明：尺寸、时长、文本字数", () => {
    expect(
      outputCaption({ media_type: "video", width: 1280, height: 720, duration_ms: 5000 }),
    ).toBe("1280×720 · 5s");
    expect(outputCaption({ media_type: "image", width: 1024, height: 1024 })).toBe("1024×1024");
    expect(outputCaption({ media_type: "text", text: "你好世界" })).toBe("4 字");
    expect(outputCaption({ media_type: "audio" })).toBe("");
  });

  test("秒级时间：月-日 时:分:秒；空值破折号", () => {
    expect(formatDateTimeSec(null)).toBe("—");
    expect(formatDateTimeSec("not-a-time")).toBe("not-a-time");
    expect(formatDateTimeSec(new Date(2026, 9, 18, 14, 5, 9).toISOString())).toBe("10-18 14:05:09");
  });

  test("detail 键值：对象值展开成 键：值，数组与空值也能显示", () => {
    expect(
      detailEntries({
        delta: 100,
        note: "补偿",
        limit: { from: 4, to: 8 },
        list: [1, 2],
        none: null,
      }),
    ).toEqual([
      ["delta", "100"],
      ["note", "补偿"],
      ["limit", "from：4，to：8"],
      ["list", "[1,2]"],
      ["none", "—"],
    ]);
    expect(detailEntries(null)).toEqual([]);
  });
});
