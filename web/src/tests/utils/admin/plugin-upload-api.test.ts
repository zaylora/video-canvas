import { afterEach, beforeEach, describe, expect, spyOn, test } from "bun:test";

import { uploadPlugin } from "@/api/admin/ai";
import type { PluginUploadResult } from "@/api/admin/ai/type";
import service from "@/utils/requests/service";

const file = () => new File(["export const meta = {}"], "demo.js");

const version = {
  id: 7,
  plugin_key: "yswg-image",
  version: "1.3.2",
  sha256: "abc",
  created_at: "",
  created_by: 1,
  channel_count: 0,
  meta: null,
};

describe("uploadPlugin：上传结果透传", () => {
  let upload: ReturnType<typeof spyOn>;
  beforeEach(() => {
    upload = spyOn(service, "upload");
  });
  // service 是全仓共用的单例，必须还原，不能留给别的测试文件
  afterEach(() => {
    upload.mockRestore();
  });

  test("版本号被自动调整时，version_adjusted 原样带出来", async () => {
    upload.mockResolvedValue({
      accepted: true,
      issues: [],
      version,
      version_adjusted: { from: "1.3.1", to: "1.3.2" },
    } satisfies PluginUploadResult);
    const result = await uploadPlugin(file());
    expect(result.accepted).toBe(true);
    expect(result.version?.version).toBe("1.3.2");
    expect(result.version_adjusted).toEqual({ from: "1.3.1", to: "1.3.2" });
  });

  test("代码里没写版本号时 from 是空串，同样保留", async () => {
    upload.mockResolvedValue({
      accepted: true,
      issues: [],
      version,
      version_adjusted: { from: "", to: "1.0.0" },
    } satisfies PluginUploadResult);
    expect((await uploadPlugin(file())).version_adjusted).toEqual({ from: "", to: "1.0.0" });
  });

  test("没有调整时后端不返回这个字段：结果里是 null，不是 undefined 以外的怪值", async () => {
    upload.mockResolvedValue({ accepted: true, issues: [], version });
    expect((await uploadPlugin(file())).version_adjusted ?? null).toBeNull();
  });

  test("预检不通过：问题列表照常带出，没有版本也没有调整", async () => {
    upload.mockResolvedValue({
      accepted: false,
      issues: [{ path: "meta", message: "代码与 v1.3.1 完全相同，没有产生新版本" }],
      version: null,
    });
    const result = await uploadPlugin(file());
    expect(result.accepted).toBe(false);
    expect(result.issues).toEqual([
      { path: "meta", message: "代码与 v1.3.1 完全相同，没有产生新版本" },
    ]);
    expect(result.version).toBeNull();
    expect(result.version_adjusted ?? null).toBeNull();
  });

  test("接口返回 null 也不崩", async () => {
    upload.mockResolvedValue(null);
    expect(await uploadPlugin(file())).toMatchObject({
      accepted: false,
      issues: [],
      version: null,
    });
  });
});
