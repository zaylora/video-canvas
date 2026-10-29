import { describe, expect, test } from "bun:test";

import {
  MAX_ATTACH_DOCUMENTS,
  MAX_DOCUMENT_BYTES,
  documentLabel,
  formatFileSize,
  pickDocumentFiles,
} from "@/utils/agent/document-files";

const file = (name: string, size = 100) => new File(["x".repeat(size)], name);

describe("文件大小显示", () => {
  test("不足 1KB 用字节，之后用 KB，≥1MB 用 MB，保留两位小数", () => {
    expect(formatFileSize(0)).toBe("0B");
    expect(formatFileSize(999)).toBe("999B");
    expect(formatFileSize(1024)).toBe("1.00KB");
    expect(formatFileSize(11_683)).toBe("11.41KB");
    expect(formatFileSize(1024 * 1024)).toBe("1.00MB");
    expect(formatFileSize(5.5 * 1024 * 1024)).toBe("5.50MB");
  });
});

describe("文件类型标签", () => {
  test("按扩展名给 TXT / MD，不区分大小写", () => {
    expect(documentLabel("剧本.txt")).toBe("TXT");
    expect(documentLabel("README.MD")).toBe("MD");
    expect(documentLabel("a.markdown")).toBe("MD");
  });
  test("没有扩展名时回退成 TXT", () => {
    expect(documentLabel("无后缀")).toBe("TXT");
  });
});

describe("挑选要上传的文档", () => {
  test("只收 .txt / .md / .markdown，其他的计入 wrongType", () => {
    const r = pickDocumentFiles([file("a.txt"), file("b.md"), file("c.pdf"), file("d.png")], 0);
    expect(r.accepted.map((f) => f.name)).toEqual(["a.txt", "b.md"]);
    expect(r.wrongType).toBe(2);
  });

  test("超过 20 万字节的不收，计入 tooLarge", () => {
    const r = pickDocumentFiles(
      [file("big.txt", MAX_DOCUMENT_BYTES + 1), file("ok.txt", MAX_DOCUMENT_BYTES)],
      0,
    );
    expect(r.accepted.map((f) => f.name)).toEqual(["ok.txt"]);
    expect(r.tooLarge).toBe(1);
  });

  test("空文件不收", () => {
    const r = pickDocumentFiles([file("empty.txt", 0)], 0);
    expect(r.accepted).toEqual([]);
    expect(r.empty).toBe(1);
  });

  test("一条消息最多 3 个文档：托盘里已有的算在内，多出的记在 clipped", () => {
    const r = pickDocumentFiles(
      [file("1.txt"), file("2.txt"), file("3.txt")],
      MAX_ATTACH_DOCUMENTS - 1,
    );
    expect(r.accepted.map((f) => f.name)).toEqual(["1.txt"]);
    expect(r.clipped).toBe(2);
  });

  test("没有文件（null）：全是 0", () => {
    expect(pickDocumentFiles(null, 0)).toEqual({
      accepted: [],
      wrongType: 0,
      tooLarge: 0,
      empty: 0,
      clipped: 0,
    });
  });
});
