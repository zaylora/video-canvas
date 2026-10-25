import { afterEach, beforeEach, describe, expect, test } from "bun:test";

import {
  downloadJson,
  exportFileName,
  MAX_IMPORT_BYTES,
  precheckImportText,
} from "@/utils/canvas/transfer-file";

describe("exportFileName", () => {
  test("非法文件名字符换成下划线并加后缀", () => {
    expect(exportFileName("a/b:c")).toBe("a_b_c.canvas.json");
    expect(exportFileName('a\\b*c?d"e<f>g|h')).toBe("a_b_c_d_e_f_g_h.canvas.json");
  });

  test("控制字符换成下划线", () => {
    expect(exportFileName("a\u0000b\nc")).toBe("a_b_c.canvas.json");
  });

  test("去掉首尾空白", () => {
    expect(exportFileName("  雨夜短片  ")).toBe("雨夜短片.canvas.json");
  });

  test("空标题和纯空白标题回退为未命名画布", () => {
    expect(exportFileName("")).toBe("未命名画布.canvas.json");
    expect(exportFileName("   \t ")).toBe("未命名画布.canvas.json");
  });

  test("三个字的标题原样保留", () => {
    expect(exportFileName("雨夜夜")).toBe("雨夜夜.canvas.json");
  });
});

describe("precheckImportText", () => {
  const valid = JSON.stringify({ format: "video-canvas/canvas", version: 1, canvas: {} });

  test("合法文件返回解析后的文档", () => {
    const result = precheckImportText(valid.length, valid);
    expect(result).toEqual({
      ok: true,
      document: { format: "video-canvas/canvas", version: 1, canvas: {} },
    });
  });

  test("刚好 10 MB 允许，超过则拒绝", () => {
    expect(MAX_IMPORT_BYTES).toBe(10 * 1024 * 1024);
    expect(precheckImportText(MAX_IMPORT_BYTES, valid).ok).toBe(true);
    expect(precheckImportText(MAX_IMPORT_BYTES + 1, valid)).toEqual({
      ok: false,
      message: "文件过大，上限 10 MB",
    });
  });

  test("不是 JSON", () => {
    expect(precheckImportText(5, "not json")).toEqual({
      ok: false,
      message: "不是有效的 JSON 文件",
    });
  });

  test("format 缺失、不对或顶层不是对象", () => {
    const notOurs = { ok: false, message: "不是本系统导出的画布文件" };
    expect(precheckImportText(2, "{}")).toEqual(notOurs);
    expect(precheckImportText(20, '{"format":"other","version":1}')).toEqual(notOurs);
    expect(precheckImportText(2, "[]")).toEqual(notOurs);
    expect(precheckImportText(4, "null")).toEqual(notOurs);
    expect(precheckImportText(2, "42")).toEqual(notOurs);
  });

  test("version 大于 1 或不是数字", () => {
    const newer = { ok: false, message: "文件来自更新版本，请升级后再导入" };
    expect(precheckImportText(40, '{"format":"video-canvas/canvas","version":2}')).toEqual(newer);
    expect(precheckImportText(40, '{"format":"video-canvas/canvas","version":"1"}')).toEqual(newer);
    expect(precheckImportText(40, '{"format":"video-canvas/canvas"}')).toEqual(newer);
  });
});

describe("downloadJson", () => {
  const g = globalThis as Record<string, unknown>;
  const saved = {
    document: g.document,
    createObjectURL: URL.createObjectURL,
    revokeObjectURL: URL.revokeObjectURL,
    setTimeout: g.setTimeout,
  };
  const events: string[] = [];
  let link: { href: string; download: string; click: () => void; remove: () => void };
  let blob: Blob | undefined;

  beforeEach(() => {
    events.length = 0;
    blob = undefined;
    link = {
      href: "",
      download: "",
      click: () => events.push("click"),
      remove: () => events.push("remove"),
    };
    g.document = {
      createElement: (tag: string) => {
        events.push(`create:${tag}`);
        return link;
      },
      body: { append: () => events.push("append") },
    };
    URL.createObjectURL = (b: Blob | MediaSource) => {
      blob = b as Blob;
      events.push("createObjectURL");
      return "blob:fake";
    };
    URL.revokeObjectURL = (url: string) => events.push(`revoke:${url}`);
    // 把延迟回收改成立即执行，免得测试等定时器
    g.setTimeout = (fn: () => void) => {
      fn();
      return 0;
    };
  });
  afterEach(() => {
    g.document = saved.document;
    URL.createObjectURL = saved.createObjectURL;
    URL.revokeObjectURL = saved.revokeObjectURL;
    g.setTimeout = saved.setTimeout;
  });

  test("用 Blob 和临时链接下载 JSON，之后回收对象地址", async () => {
    downloadJson("a.canvas.json", { a: 1 });

    expect(link.download).toBe("a.canvas.json");
    expect(link.href).toBe("blob:fake");
    expect(blob?.type.startsWith("application/json")).toBe(true);
    expect(JSON.parse(await blob!.text())).toEqual({ a: 1 });
    expect(events.indexOf("click")).toBeGreaterThan(events.indexOf("create:a"));
    expect(events.indexOf("remove")).toBeGreaterThan(events.indexOf("click"));
    expect(events.at(-1)).toBe("revoke:blob:fake");
  });
});
