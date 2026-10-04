import { describe, expect, test } from "bun:test";

import type {
  ProcessorCheck,
  ProcessorConfig,
  ProcessorPreset,
  ProcessorView,
} from "@/api/admin-image-processor/type";
import {
  bindingOfStorage,
  buildProcessorConfig,
  checkIsStale,
  configFields,
  defaultConfig,
  enableTarget,
  evaluateStorage,
  formatBytes,
  hostOf,
  isOwnStorageVendor,
  processorActions,
  publishBlockReason,
  trialLine,
  trialSummary,
  validateProcessorForm,
  type BindableStorage,
} from "@/utils/admin/image-processor";
import { ADMIN_ERROR_CODE, isProcessorVersionConflict } from "@/utils/admin/errors";

const baseConfig: ProcessorConfig = { domain: "", width: 512, format: "jpg", time_sec: 0 };

const presets: ProcessorPreset[] = [
  {
    vendor: "tencent_ci",
    name: "腾讯云 数据万象",
    storage_provider: "tencent_cos",
    requires_public_base: false,
    supports_poster: true,
    formats: ["jpg", "webp"],
    default_config: { ...baseConfig, media_enabled: false },
  },
  {
    vendor: "aliyun_oss_img",
    name: "阿里云 OSS 图片处理",
    storage_provider: "aliyun_oss",
    requires_public_base: false,
    supports_poster: true,
    formats: ["jpg", "webp"],
    default_config: baseConfig,
  },
  {
    vendor: "cloudflare",
    name: "Cloudflare R2",
    storage_provider: "r2",
    requires_public_base: true,
    supports_poster: true,
    formats: ["auto", "webp"],
    default_config: { ...baseConfig, format: "auto", quality: 75, on_error_redirect: true },
  },
];
const [tencent, aliyun, cloudflare] = presets;

const storage = (patch: Partial<BindableStorage> = {}): BindableStorage => ({
  id: 1,
  name: "存储",
  provider: "r2",
  bucket: "vc-prod",
  region: "auto",
  public_base_url: "https://assets.example.com",
  ...patch,
});

const proc = (patch: Partial<ProcessorView> = {}): ProcessorView => ({
  id: 10,
  name: "处理服务",
  vendor: "cloudflare",
  storage_id: 1,
  storage_name: "存储",
  storage_provider: "r2",
  status: "draft",
  config: { ...baseConfig, domain: "assets.example.com" },
  published_config: null,
  has_draft: false,
  published_version: 0,
  previous_version: null,
  version: 3,
  check: null,
  updated_at: "",
  created_at: "",
  ...patch,
});

const check = (patch: Partial<ProcessorCheck> = {}): ProcessorCheck => ({
  ok: true,
  version: 3,
  checked_at: "",
  checks: [],
  trial: { image: null, video: null },
  ...patch,
});

describe("hostOf", () => {
  test("取 URL 的 host，非法或空串返回空串", () => {
    expect(hostOf("https://assets.example.com/a/b")).toBe("assets.example.com");
    expect(hostOf("https://cdn.example.com:8443")).toBe("cdn.example.com:8443");
    expect(hostOf("")).toBe("");
    expect(hostOf("not a url")).toBe("");
  });
});

describe("evaluateStorage：存储与厂商的兼容判定", () => {
  test("provider 匹配且无冲突时可选", () => {
    expect(evaluateStorage(cloudflare, storage(), [])).toEqual({
      selectable: true,
      reason: null,
      note: null,
    });
  });

  test("provider 不匹配时禁用，原因写明存储类型与厂商要求", () => {
    const result = evaluateStorage(tencent, storage({ provider: "aliyun_oss" }), []);
    expect(result.selectable).toBe(false);
    expect(result.reason).toContain("阿里云 OSS");
    expect(result.reason).toContain("腾讯云 COS");
  });

  test("本地磁盘与 s3 对三家厂商都不匹配", () => {
    for (const preset of presets) {
      expect(evaluateStorage(preset, storage({ provider: "local" }), []).selectable).toBe(false);
      expect(evaluateStorage(preset, storage({ provider: "s3" }), []).selectable).toBe(false);
    }
  });

  test("cloudflare 要求存储有公开域名，其他厂商不要求", () => {
    const noBase = storage({ public_base_url: "" });
    const blocked = evaluateStorage(cloudflare, noBase, []);
    expect(blocked.selectable).toBe(false);
    expect(blocked.reason).toContain("公开域名");
    expect(
      evaluateStorage(tencent, storage({ provider: "tencent_cos", public_base_url: "" }), [])
        .selectable,
    ).toBe(true);
  });

  test("已被另一个已发布的处理服务占用只给提示，仍可选", () => {
    const result = evaluateStorage(cloudflare, storage(), [
      proc({ id: 99, name: "旧服务", status: "published" }),
    ]);
    expect(result.selectable).toBe(true);
    expect(result.reason).toBeNull();
    expect(result.note).toContain("旧服务");
  });

  test("占用的是自己、草稿或已停用的，不提示", () => {
    expect(
      evaluateStorage(cloudflare, storage(), [proc({ id: 10, status: "published" })], 10).note,
    ).toBeNull();
    expect(
      evaluateStorage(cloudflare, storage(), [proc({ id: 99, status: "draft" })]).note,
    ).toBeNull();
    expect(
      evaluateStorage(cloudflare, storage(), [proc({ id: 99, status: "disabled" })]).note,
    ).toBeNull();
  });
});

describe("enableTarget：存储表的“启用处理服务”", () => {
  test("本地磁盘禁用并写明原因", () => {
    const target = enableTarget(storage({ provider: "local" }), presets, []);
    expect(target.preset).toBeNull();
    expect(target.reason).toBe("本地磁盘的地址厂商访问不到，无法接入处理服务");
  });

  test("没有对应厂商的存储类型（s3）禁用", () => {
    const target = enableTarget(storage({ provider: "s3" }), presets, []);
    expect(target.preset).toBeNull();
    expect(target.reason).toBeTruthy();
  });

  test("r2 没有公开域名时禁用，提示去存储配置设置", () => {
    const target = enableTarget(storage({ public_base_url: "" }), presets, []);
    expect(target.preset).toBeNull();
    expect(target.reason).toContain("公开域名");
  });

  test("匹配时给出预设，可直接启用", () => {
    const target = enableTarget(storage({ provider: "aliyun_oss" }), presets, []);
    expect(target.reason).toBeNull();
    expect(target.preset?.vendor).toBe("aliyun_oss_img");
  });
});

describe("bindingOfStorage", () => {
  test("分出已发布、草稿、已停用", () => {
    const list = [
      proc({ id: 1, storage_id: 5, status: "disabled" }),
      proc({ id: 2, storage_id: 5, status: "published" }),
      proc({ id: 3, storage_id: 5, status: "draft" }),
      proc({ id: 4, storage_id: 6, status: "published" }),
    ];
    const result = bindingOfStorage(5, list);
    expect(result.published?.id).toBe(2);
    expect(result.draft?.id).toBe(3);
    expect(result.disabled?.id).toBe(1);
    expect(bindingOfStorage(7, list)).toEqual({ published: null, draft: null, disabled: null });
  });
});

describe("defaultConfig：默认配置推导", () => {
  test("cloudflare 的 domain 取存储公开域名的 host", () => {
    expect(defaultConfig(cloudflare, storage()).domain).toBe("assets.example.com");
    expect(defaultConfig(cloudflare, storage()).quality).toBe(75);
  });

  test("cloudflare 存储没有公开域名时 domain 为空", () => {
    expect(defaultConfig(cloudflare, storage({ public_base_url: "" })).domain).toBe("");
  });

  test("腾讯云用 bucket 与 region 拼 COS 默认域名", () => {
    const st = storage({
      provider: "tencent_cos",
      bucket: "canvas-1250000000",
      region: "ap-seoul",
    });
    expect(defaultConfig(tencent, st).domain).toBe("canvas-1250000000.cos.ap-seoul.myqcloud.com");
  });

  test("腾讯云缺 bucket 或 region 时不拼，保留预设默认", () => {
    expect(defaultConfig(tencent, storage({ provider: "tencent_cos", region: "" })).domain).toBe(
      "",
    );
  });

  test("阿里云沿用预设默认，不修改预设对象", () => {
    const result = defaultConfig(aliyun, storage({ provider: "aliyun_oss" }));
    expect(result).toEqual(aliyun.default_config);
    result.width = 1;
    expect(aliyun.default_config.width).toBe(512);
  });
});

describe("configFields：按厂商与预设决定显示哪些字段", () => {
  test("cloudflare 有质量与回退原图，没有媒体处理开关", () => {
    expect(configFields(cloudflare)).toMatchObject({
      time: true,
      quality: true,
      onErrorRedirect: true,
      mediaEnabled: false,
    });
  });

  test("腾讯云有媒体处理开关", () => {
    expect(configFields(tencent)).toMatchObject({
      quality: false,
      onErrorRedirect: false,
      mediaEnabled: true,
    });
  });

  test("预设不支持封面时不显示取帧时间，也不显示媒体处理开关", () => {
    const noPoster = { ...tencent, supports_poster: false };
    expect(configFields(noPoster)).toMatchObject({ time: false, mediaEnabled: false });
  });
});

describe("buildProcessorConfig：提交前裁掉不适用的字段", () => {
  test("trim 域名；非 cloudflare 去掉 quality 与 on_error_redirect", () => {
    const body = buildProcessorConfig(aliyun, {
      ...baseConfig,
      domain: "  img.example.com ",
      quality: 80,
      on_error_redirect: true,
      media_enabled: true,
    });
    expect(body).toEqual({ domain: "img.example.com", width: 512, format: "jpg", time_sec: 0 });
  });

  test("cloudflare 保留 quality 与 on_error_redirect", () => {
    const body = buildProcessorConfig(cloudflare, {
      ...cloudflare.default_config,
      domain: "a.example.com",
    });
    expect(body.quality).toBe(75);
    expect(body.on_error_redirect).toBe(true);
    expect("media_enabled" in body).toBe(false);
  });

  test("不支持封面时 time_sec 置 0", () => {
    const body = buildProcessorConfig(
      { ...aliyun, supports_poster: false },
      { ...baseConfig, domain: "a.b", time_sec: 3 },
    );
    expect(body.time_sec).toBe(0);
  });
});

describe("validateProcessorForm：表单校验", () => {
  const ok = { name: "n", config: { ...baseConfig, domain: "img.example.com" } };

  test("合法时没有错误", () => {
    expect(validateProcessorForm(aliyun, ok)).toEqual({});
  });

  test("名称与域名必填", () => {
    const errors = validateProcessorForm(aliyun, {
      name: " ",
      config: { ...baseConfig, domain: " " },
    });
    expect(errors.name).toBeTruthy();
    expect(errors.domain).toBeTruthy();
  });

  test("域名只写 host：带协议或路径报错", () => {
    expect(
      validateProcessorForm(aliyun, { ...ok, config: { ...ok.config, domain: "https://a.com" } })
        .domain,
    ).toBeTruthy();
    expect(
      validateProcessorForm(aliyun, { ...ok, config: { ...ok.config, domain: "a.com/x" } }).domain,
    ).toBeTruthy();
  });

  test("width 需为 16 到 2000 的整数，边界值合法", () => {
    const withWidth = (width: number) =>
      validateProcessorForm(aliyun, { ...ok, config: { ...ok.config, width } }).width;
    expect(withWidth(15)).toBeTruthy();
    expect(withWidth(2001)).toBeTruthy();
    expect(withWidth(Number.NaN)).toBeTruthy();
    expect(withWidth(512.5)).toBeTruthy();
    expect(withWidth(16)).toBeUndefined();
    expect(withWidth(2000)).toBeUndefined();
  });

  test("取帧时间不能为负；不支持封面时不校验", () => {
    const bad = { ...ok, config: { ...ok.config, time_sec: -1 } };
    expect(validateProcessorForm(aliyun, bad).time_sec).toBeTruthy();
    expect(
      validateProcessorForm({ ...aliyun, supports_poster: false }, bad).time_sec,
    ).toBeUndefined();
  });

  test("cloudflare 的 quality 需为 1 到 100", () => {
    const withQuality = (quality: number) =>
      validateProcessorForm(cloudflare, { ...ok, config: { ...ok.config, quality } }).quality;
    expect(withQuality(0)).toBeTruthy();
    expect(withQuality(101)).toBeTruthy();
    expect(withQuality(75)).toBeUndefined();
    expect(
      validateProcessorForm(aliyun, { ...ok, config: { ...ok.config, quality: 0 } }).quality,
    ).toBeUndefined();
  });
});

describe("processorActions：状态到按钮可用性", () => {
  test("草稿：可编辑、校验发布、删除；不能回滚、停用", () => {
    expect(processorActions(proc({ status: "draft" }), true)).toEqual({
      edit: true,
      verify: true,
      rollback: false,
      disable: false,
      remove: true,
    });
  });

  test("已发布且无草稿：不能再发布，不能删除；有上一版才能回滚", () => {
    const a = processorActions(proc({ status: "published", previous_version: null }), true);
    expect(a).toMatchObject({ verify: false, rollback: false, disable: true, remove: false });
    expect(
      processorActions(proc({ status: "published", previous_version: 2 }), true).rollback,
    ).toBe(true);
  });

  test("已发布且有草稿：可校验发布", () => {
    expect(processorActions(proc({ status: "published", has_draft: true }), true).verify).toBe(
      true,
    );
  });

  test("已停用：可编辑、校验发布（重新启用）、删除", () => {
    expect(processorActions(proc({ status: "disabled" }), true)).toMatchObject({
      verify: true,
      disable: false,
      remove: true,
    });
  });

  test("没有写权限时全部为 false", () => {
    expect(
      Object.values(
        processorActions(
          proc({ status: "published", has_draft: true, previous_version: 1 }),
          false,
        ),
      ),
    ).toEqual([false, false, false, false, false]);
  });
});

describe("发布前置条件", () => {
  test("没有校验结果时不能发布", () => {
    expect(checkIsStale(proc())).toBe(true);
    expect(publishBlockReason(proc())).toContain("校验");
  });

  test("check.version 落后于 version 时需要重新校验", () => {
    const p = proc({ version: 4, check: check({ version: 3 }) });
    expect(checkIsStale(p)).toBe(true);
    expect(publishBlockReason(p)).toContain("重新校验");
  });

  test("有 fail 时不能发布", () => {
    expect(publishBlockReason(proc({ check: check({ ok: false }) }))).toContain("未通过");
  });

  test("check.ok 且 version 一致可以发布，warn 不挡", () => {
    const p = proc({
      check: check({ checks: [{ key: "media", label: "媒体处理", status: "warn", message: "" }] }),
    });
    expect(checkIsStale(p)).toBe(false);
    expect(publishBlockReason(p)).toBeNull();
  });

  test("已发布且没有草稿时没有可发布的内容", () => {
    const p = proc({ status: "published", has_draft: false, check: check() });
    expect(publishBlockReason(p)).toContain("没有");
  });

  test("已发布且有草稿，校验通过可以发布", () => {
    expect(
      publishBlockReason(proc({ status: "published", has_draft: true, check: check() })),
    ).toBeNull();
  });
});

describe("展示辅助", () => {
  test("formatBytes 按量级换算", () => {
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(38 * 1024)).toBe("38 KB");
    expect(formatBytes(3.1 * 1024 * 1024)).toBe("3.1 MB");
  });

  test("trialSummary：有素材给体积耗时，没有素材说明已跳过", () => {
    const summary = trialSummary(
      check({
        trial: { image: { bytes: 38 * 1024, ms: 286, source_bytes: 3 * 1024 * 1024 }, video: null },
      }),
    );
    expect(summary.image).toBe("3 MB → 38 KB · 286 ms");
    expect(summary.video).toBeNull();
    expect(trialSummary(null)).toEqual({ image: null, video: null });
  });

  test("trialLine：试跑成功给体积耗时；没有结果时如实说明原因，不一律说“没有素材”", () => {
    const item = (key: string, status: "ok" | "warn" | "fail", message: string) => ({
      key,
      label: key,
      status,
      message,
    });
    // 成功
    expect(
      trialLine(
        check({
          trial: {
            image: { bytes: 38 * 1024, ms: 286, source_bytes: 3 * 1024 * 1024 },
            video: null,
          },
        }),
        "image",
      ),
    ).toEqual({ text: "3 MB → 38 KB · 286 ms", tone: "ok" });
    // 失败：沿用校验项里的原因，而不是说“没有素材”
    expect(
      trialLine(
        check({ checks: [item("trial_image", "fail", "请求失败：目标地址不允许访问")] }),
        "image",
      ),
    ).toEqual({ text: "请求失败：目标地址不允许访问", tone: "fail" });
    // 提示（没有素材 / 不支持）：沿用后端给的说明
    expect(
      trialLine(
        check({ checks: [item("trial_video", "warn", "这套存储里还没有视频素材，没法试跑")] }),
        "video",
      ),
    ).toEqual({ text: "这套存储里还没有视频素材，没法试跑", tone: "warn" });
    // 没有对应校验项（旧数据或没校验过）
    expect(trialLine(check(), "image")).toEqual({ text: "未试跑", tone: "none" });
    expect(trialLine(null, "video")).toEqual({ text: "未试跑", tone: "none" });
  });

  test("isOwnStorageVendor：腾讯云、阿里云必须使用自家存储", () => {
    expect(isOwnStorageVendor("tencent_ci")).toBe(true);
    expect(isOwnStorageVendor("aliyun_oss_img")).toBe(true);
    expect(isOwnStorageVendor("cloudflare")).toBe(false);
  });
});

describe("版本冲突错误码", () => {
  test("52004 被识别为版本冲突", () => {
    expect(ADMIN_ERROR_CODE.PROCESSOR_VERSION_CONFLICT).toBe(52004);
    expect(isProcessorVersionConflict({ code: 52004 })).toBe(true);
    expect(isProcessorVersionConflict({ code: 51009 })).toBe(false);
  });
});
