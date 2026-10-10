import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const SRC = join(import.meta.dir, "../../..");

/** 后台用到的源码：页面与后台专用组件；Windows 下扫出来是反斜杠，统一成正斜杠好做路径判断 */
const FILES = [
  ...new Bun.Glob("pages/admin/**/*.tsx").scanSync({ cwd: SRC }),
  ...new Bun.Glob("components/admin-ui/**/*.tsx").scanSync({ cwd: SRC }),
].map((file) => file.replaceAll("\\", "/"));

/** 找出写死像素字号（text-[13px] 这种）的位置，返回「文件:行 字号」 */
function pixelSizes(filter: (file: string, px: string) => boolean) {
  const found: string[] = [];
  for (const file of FILES) {
    const lines = readFileSync(join(SRC, file), "utf8").split("\n");
    lines.forEach((line, index) => {
      for (const match of line.matchAll(/text-\[([0-9.]+)px\]/g)) {
        if (filter(file, match[1])) found.push(`${file}:${index + 1} text-[${match[1]}px]`);
      }
    });
  }
  return found;
}

describe("后台字号收敛", () => {
  test("正文、标题不写 12.5 / 13 / 13.5 / 15 / 17px 这类自定义字号，统一用 text-xs / text-sm / text-base 刻度", () => {
    const stray = pixelSizes(
      (file, px) =>
        ["12.5", "13", "13.5", "15", "17"].includes(px) &&
        // 登录页展示里那块模拟舞台，字号要和真实登录页的品牌字一致
        !(file.endsWith("settings/showcase/index.tsx") && px === "17"),
    );
    expect(stray).toEqual([]);
  });

  test("徽标、角标这类小字统一用 11px；10px 只留在模型页（后台字号以模型页为准）", () => {
    const stray = pixelSizes(
      (file, px) => px === "10" && !file.startsWith("pages/admin/ai/models/"),
    );
    expect(stray).toEqual([]);
  });
});
