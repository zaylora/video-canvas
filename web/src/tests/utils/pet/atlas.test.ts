import { describe, expect, test } from "bun:test";

import { buildKeyframes } from "@/utils/pet/atlas";
import { PET_CELL, PET_ROWS } from "@/utils/pet/rows";

const css = buildKeyframes(PET_ROWS, PET_CELL);

/** 取出某一行的 @keyframes 块内容 */
const block = (row: number) => {
  const m = css.match(new RegExp(`@keyframes pet-row-${row}\\{([^@]*?)\\}\\n`));
  expect(m).not.toBeNull();
  return m![1]!;
};

describe("buildKeyframes", () => {
  test("每行一个 @keyframes，10 行都在", () => {
    for (const r of PET_ROWS) expect(css).toContain(`@keyframes pet-row-${r.row}{`);
  });

  test("idle：百分比按帧时长累加，位置按列与行换算", () => {
    const b = block(0);
    // 总长 1100ms：起点 0 / 280 / 390 / 500 / 640 / 780
    expect(b).toContain("0%{background-position:0px 0px}");
    expect(b).toContain("25.4545%{background-position:-192px 0px}");
    expect(b).toContain("35.4545%{background-position:-384px 0px}");
    expect(b).toContain("45.4545%{background-position:-576px 0px}");
    expect(b).toContain("58.1818%{background-position:-768px 0px}");
    expect(b).toContain("70.9091%{background-position:-960px 0px}");
  });

  test("最后一帧停留更久：收尾的 100% 保持最后一帧", () => {
    const b = block(0);
    expect(b).toContain("100%{background-position:-960px 0px}");
    // 最后一帧从 70.9091% 持续到 100%：约 320ms
    expect(((100 - 70.9091) / 100) * 1100).toBeCloseTo(320, 1);
  });

  test("行号换算成纵向偏移：failed 在第 5 行", () => {
    expect(block(5)).toContain("0%{background-position:0px -1040px}");
  });

  test("帧数正确：每行的关键帧个数是帧数加一个收尾", () => {
    for (const r of PET_ROWS) {
      const n = block(r.row).split("%{").length - 1;
      expect(n).toBe(r.frames + 1);
    }
  });

  test("只用到每行已有的帧：waving 只有 4 帧，不会出现第 5 列", () => {
    expect(block(3)).not.toContain("-768px");
  });

  test("没有 sleeping 行时不生成第 9 行", () => {
    const nine = buildKeyframes(PET_ROWS.slice(0, 9), PET_CELL);
    expect(nine).not.toContain("pet-row-9");
  });

  test("按 data-row 选中动画：名称、总时长，阶梯函数 step-end", () => {
    expect(css).toContain(
      '.pet-sprite[data-row="0"]{animation-name:pet-row-0;animation-duration:1100ms}',
    );
    expect(css).toContain(
      '.pet-sprite[data-row="9"]{animation-name:pet-row-9;animation-duration:1500ms}',
    );
    expect(css).toContain("animation-timing-function:step-end");
  });

  test("播一轮、暂停、减少动态效果的规则", () => {
    expect(css).toContain(
      '.pet-sprite[data-loop="false"]{animation-iteration-count:1;animation-fill-mode:forwards}',
    );
    expect(css).toContain('.pet-sprite[data-paused="true"]{animation-play-state:paused}');
    expect(css).toContain("@media (prefers-reduced-motion:reduce)");
    expect(css).toContain('.pet-sprite[data-row="5"]{background-position:0px -1040px}');
  });

  test("可以指定选择器", () => {
    const x = buildKeyframes(PET_ROWS.slice(0, 1), PET_CELL, { selector: ".foo" });
    expect(x).toContain('.foo[data-row="0"]');
    expect(x).not.toContain(".pet-sprite");
  });
});
