import { afterAll, beforeEach, describe, expect, mock, test } from "bun:test";

import type { CharacterDto } from "@/api/character/type";

const card = (id: string, name = id): CharacterDto => ({
  id,
  name,
  description: "",
  avatar: null,
  extras: [],
  voice: null,
  createdAt: "",
  updatedAt: "",
});

const fetched: string[][] = [];
let respond: (ids: string[]) => Promise<CharacterDto[]> = async (ids) => ids.map((id) => card(id));

// mock.module 在整个测试进程里生效且不会自动还原：只给部分导出，后面加载真实模块的测试文件
// 会报 Export named 'xxx' not found（取决于文件执行顺序，macOS 与 CI 的顺序不同）。
// 所以先留一份真实导出，mock 时展开，文件结束后再还原
const realApi = { ...(await import("@/api/character")) };
mock.module("@/api/character", () => ({
  ...realApi,
  getCharactersByIds: (ids: string[]) => {
    fetched.push(ids);
    return respond(ids);
  },
}));
afterAll(() => {
  mock.module("@/api/character", () => ({ ...realApi }));
});
// mock.module 必须先于被测模块加载，静态 import 会被提升到它前面，所以这里只能动态导入
const { useCharactersStore, CHARACTER_TTL } = await import("@/store/characters");

const entry = (id: string) => useCharactersStore.getState().byId[id];

beforeEach(() => {
  fetched.length = 0;
  respond = async (ids) => ids.map((id) => card(id));
  useCharactersStore.setState({ byId: {} });
});

describe("useCharactersStore：按 ID 批量读并缓存", () => {
  test("同一时刻要的多张卡合成一次请求，回来前是 loading", async () => {
    const pending = Promise.all([
      useCharactersStore.getState().ensure(["a", "b"]),
      useCharactersStore.getState().ensure(["b", "c"]),
    ]);
    expect(entry("a")?.status).toBe("loading");
    await pending;
    expect(fetched).toEqual([["a", "b", "c"]]);
    expect(entry("c")?.status).toBe("ready");
    expect(entry("c")?.card?.name).toBe("c");
  });

  test("已缓存且新鲜的不再请求；过期的后台重读且不退回 loading", async () => {
    await useCharactersStore.getState().ensure(["a"]);
    await useCharactersStore.getState().ensure(["a"]);
    expect(fetched).toHaveLength(1);
    useCharactersStore.setState({
      byId: { a: { ...entry("a"), loadedAt: Date.now() - CHARACTER_TTL - 1 } },
    });
    const again = useCharactersStore.getState().ensure(["a"]);
    expect(entry("a")?.status).toBe("ready");
    await again;
    expect(fetched).toHaveLength(2);
  });

  test("后端没返回的 ID 记为 missing（节点据此显示已失效）", async () => {
    respond = async () => [card("a")];
    await useCharactersStore.getState().ensure(["a", "gone"]);
    expect(entry("a")?.status).toBe("ready");
    expect(entry("gone")?.status).toBe("missing");
  });

  test("超过 100 个 ID 分批请求", async () => {
    const ids = Array.from({ length: 205 }, (_, i) => `c${i}`);
    await useCharactersStore.getState().ensure(ids);
    expect(fetched.map((batch) => batch.length)).toEqual([100, 100, 5]);
  });

  test("请求失败：有旧内容的保留旧卡并标 error，没有的是 error 无卡", async () => {
    await useCharactersStore.getState().ensure(["a"]);
    respond = async () => {
      throw new Error("net");
    };
    const out = await useCharactersStore.getState().refresh(["a", "b"]);
    expect(entry("a")?.status).toBe("error");
    expect(entry("a")?.card?.name).toBe("a");
    expect(entry("b")?.status).toBe("error");
    expect(entry("b")?.card).toBeUndefined();
    expect(out.a?.status).toBe("error");
  });

  test("refresh 无视缓存强制重读，返回最新状态（提交前用）", async () => {
    await useCharactersStore.getState().ensure(["a"]);
    respond = async () => [card("a", "新名字")];
    const out = await useCharactersStore.getState().refresh(["a"]);
    expect(out.a?.card?.name).toBe("新名字");
    expect(entry("a")?.card?.name).toBe("新名字");
  });

  test("资产页保存后 upsert、删除后 remove：画布节点读到的就是最新", async () => {
    await useCharactersStore.getState().ensure(["a"]);
    useCharactersStore.getState().upsert(card("a", "改名了"));
    expect(entry("a")?.card?.name).toBe("改名了");
    useCharactersStore.getState().remove("a");
    expect(entry("a")?.status).toBe("missing");
    expect(entry("a")?.card).toBeUndefined();
  });

  test("invalidate 让已缓存的卡过期：下次 ensure 重读", async () => {
    await useCharactersStore.getState().ensure(["a", "b"]);
    useCharactersStore.getState().invalidate();
    await useCharactersStore.getState().ensure(["a"]);
    expect(fetched).toHaveLength(2);
    expect(fetched[1]).toEqual(["a"]);
  });
});
