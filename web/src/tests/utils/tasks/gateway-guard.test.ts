import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, sep } from "node:path";

/** 源码根目录：tests/utils/tasks 往上三级 */
const SRC = join(import.meta.dir, "..", "..", "..");

const walk = (dir: string): string[] =>
  readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    return statSync(path).isDirectory() ? walk(path) : [path];
  });

/** 非测试的源码文件，路径统一用 / */
const sources = walk(SRC)
  .map((path) => relative(SRC, path).split(sep).join("/"))
  .filter((path) => /\.(ts|tsx)$/.test(path) && !path.startsWith("tests/"));

/** 哪些文件里出现了这个标识符 */
const filesUsing = (identifier: RegExp) =>
  sources.filter((path) => identifier.test(readFileSync(join(SRC, path), "utf8")));

describe("生成任务统一入口（utils/tasks/gateway.ts）", () => {
  test("扫描范围没有跑偏：能扫到源码，也能扫到统一入口本身", () => {
    expect(sources.length).toBeGreaterThan(100);
    expect(sources).toContain("utils/tasks/gateway.ts");
    expect(filesUsing(/\bcreateGenerationTask\b/)).toContain("utils/tasks/gateway.ts");
  });

  test("提交和取消任务的接口只有统一入口能调用，其余代码不许绕过它", () => {
    for (const name of [
      "createGenerationTask",
      "cancelGenerationTask",
      "submitConversationRecord",
    ]) {
      const users = filesUsing(new RegExp(`\\b${name}\\b`)).filter(
        // 定义它们的 api 模块本身不算
        (path) => !path.startsWith("api/") && path !== "utils/tasks/gateway.ts",
      );
      expect(users, `${name} 只能在 utils/tasks/gateway.ts 里调用`).toEqual([]);
    }
  });

  test("幂等键与重试（submitWithRetry）只在统一入口里使用", () => {
    const users = filesUsing(/\bsubmitWithRetry\b/).filter(
      (path) => path !== "utils/tasks/gateway.ts" && path !== "utils/tasks/submit.ts",
    );
    expect(users, "submitWithRetry 只能在 utils/tasks/gateway.ts 里使用").toEqual([]);
  });

  test("任务快照入库（handleTaskView）只有统一入口和 WebSocket 层能调用", () => {
    const users = filesUsing(/\bhandleTaskView\b/).filter(
      (path) => path !== "utils/tasks/gateway.ts" && !path.startsWith("utils/ws/"),
    );
    expect(users, "handleTaskView 只能经 gateway.acceptTasks 或 WebSocket 层调用").toEqual([]);
  });
});
