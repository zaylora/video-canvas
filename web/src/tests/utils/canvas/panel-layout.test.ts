import { describe, expect, test } from "bun:test";

import { PANEL_WIDTH, panelLayout } from "@/utils/canvas/panel-layout";

describe("panelLayout：生成面板的宽度和水平位移", () => {
  test("面板宽度固定 680（100% 缩放时节点宽 576 + 104）", () => {
    expect(PANEL_WIDTH).toBe(680);
    expect(panelLayout({ nodeWidth: 576, nodeLeft: 400, viewportWidth: 1369 })).toEqual({
      width: 680,
      shift: 0,
    });
  });

  test("画布缩小时面板大小不变：节点只剩 167px 宽，面板仍是 680", () => {
    expect(panelLayout({ nodeWidth: 167, nodeLeft: 640, viewportWidth: 1369 }).width).toBe(680);
  });

  test("画布放大时面板大小也不变：节点 1152px 宽，面板仍是 680", () => {
    expect(panelLayout({ nodeWidth: 1152, nodeLeft: 100, viewportWidth: 1369 }).width).toBe(680);
  });

  test("面板以节点中线为轴居中", () => {
    // 节点 167 宽、左边在 636 → 中线 719.5；面板左边应在 379.5，所以在视口内不用位移
    expect(panelLayout({ nodeWidth: 167, nodeLeft: 636, viewportWidth: 1369 }).shift).toBe(0);
  });

  test("视口比面板还窄：宽度夹到视口减去两边各 12px", () => {
    expect(panelLayout({ nodeWidth: 300, nodeLeft: 50, viewportWidth: 400 })).toEqual({
      width: 376,
      shift: 0,
    });
  });

  test("面板左边出了视口：右移到离边缘 12px", () => {
    // 节点中心在 100，面板 680 宽 → 左边在 -240，要右移 252
    const { shift } = panelLayout({ nodeWidth: 576, nodeLeft: -188, viewportWidth: 1369 });
    expect(shift).toBe(252);
  });

  test("面板右边出了视口：左移到离边缘 12px", () => {
    // 节点中心在 1300，面板 680 宽 → 右边在 1640，要左移 1640 - (1369 - 12)
    const { shift } = panelLayout({ nodeWidth: 576, nodeLeft: 1012, viewportWidth: 1369 });
    expect(shift).toBe(-283);
  });
});
