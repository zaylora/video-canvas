import { beforeEach, describe, expect, test } from "bun:test";

import { useCharacterDetail } from "@/store/character-detail";

beforeEach(() => useCharacterDetail.setState({ nodeId: null }));

describe("useCharacterDetail：请求打开某个角色卡节点的详情", () => {
  test("open 记下节点 id，close 清掉", () => {
    expect(useCharacterDetail.getState().nodeId).toBeNull();
    useCharacterDetail.getState().open("n1");
    expect(useCharacterDetail.getState().nodeId).toBe("n1");
    useCharacterDetail.getState().close();
    expect(useCharacterDetail.getState().nodeId).toBeNull();
  });

  test("再点另一个节点：换成它（同一时刻只开一个详情）", () => {
    useCharacterDetail.getState().open("n1");
    useCharacterDetail.getState().open("n2");
    expect(useCharacterDetail.getState().nodeId).toBe("n2");
  });
});
