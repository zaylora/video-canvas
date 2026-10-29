import { beforeEach, describe, expect, test } from "bun:test";

import { usePromptEditor } from "@/store/prompt-editor";

beforeEach(() => usePromptEditor.setState({ nodeId: null }));

describe("usePromptEditor：请求在弹框里编辑某个节点的生成设置", () => {
  test("open 记下节点 id，close 清掉", () => {
    expect(usePromptEditor.getState().nodeId).toBeNull();
    usePromptEditor.getState().open("n1");
    expect(usePromptEditor.getState().nodeId).toBe("n1");
    usePromptEditor.getState().close();
    expect(usePromptEditor.getState().nodeId).toBeNull();
  });

  test("再打开另一个节点：换成它（同一时刻只开一个）", () => {
    usePromptEditor.getState().open("n1");
    usePromptEditor.getState().open("n2");
    expect(usePromptEditor.getState().nodeId).toBe("n2");
  });
});
