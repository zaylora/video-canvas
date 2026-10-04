import { describe, expect, test } from "bun:test";

import { downloadName } from "@/utils/canvas/download";

describe("downloadName", () => {
  test("有上传原文件名就用它", () => {
    expect(downloadName("节点", "/files/a.png", "封面.png")).toBe("封面.png");
  });

  test("没有就用节点名加地址里的扩展名，query 和 hash 不算", () => {
    expect(downloadName("山洞篝火", "/files/ab12.mp4?v=poster#t=1")).toBe("山洞篝火.mp4");
  });

  test("地址里没有扩展名就只用节点名", () => {
    expect(downloadName("街道", "/files/ab12")).toBe("街道");
  });
});
