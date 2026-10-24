import { describe, expect, test } from "bun:test";

import {
  STAGE_INTERVAL_MS,
  TRANSFER_STAGES,
  stageStatus,
  stageIndexAt,
} from "@/utils/canvas/transfer-stages";

describe("transfer-stages：进度弹窗的阶段文案", () => {
  test("导出和导入各有三个阶段，文案按流程顺序", () => {
    expect(TRANSFER_STAGES.export).toEqual(["保存最新内容", "整理素材", "生成文件"]);
    expect(TRANSFER_STAGES.import).toEqual(["检查文件", "复制素材", "创建新画布"]);
  });

  test("阶段随时间推进，每 STAGE_INTERVAL_MS 前进一格", () => {
    expect(stageIndexAt(0, 3)).toBe(0);
    expect(stageIndexAt(STAGE_INTERVAL_MS - 1, 3)).toBe(0);
    expect(stageIndexAt(STAGE_INTERVAL_MS, 3)).toBe(1);
    expect(stageIndexAt(STAGE_INTERVAL_MS * 2, 3)).toBe(2);
  });

  test("最后一步停住：请求没返回就不会假装完成", () => {
    expect(stageIndexAt(STAGE_INTERVAL_MS * 99, 3)).toBe(2);
  });

  test("时间为负或不是有限数时按 0 处理", () => {
    expect(stageIndexAt(-5, 3)).toBe(0);
    expect(stageIndexAt(Number.NaN, 3)).toBe(0);
    expect(stageIndexAt(Number.POSITIVE_INFINITY, 3)).toBe(2);
  });

  test("没有阶段时下标是 0，不会越界", () => {
    expect(stageIndexAt(1000, 0)).toBe(0);
  });

  test("阶段状态：当前之前是 done，当前是 now，之后是 todo", () => {
    expect(stageStatus(0, 1)).toBe("done");
    expect(stageStatus(1, 1)).toBe("now");
    expect(stageStatus(2, 1)).toBe("todo");
  });
});
