import { describe, expect, test } from "bun:test";

import type { AssetDto } from "@/api/asset/type";
import type { FolderDto, GeneratedItemDto, LibraryItemDto } from "@/api/asset-library/type";
import {
  addFolder,
  adjustFolderCount,
  removeFolder,
  renameFolder,
} from "@/utils/assets/folder-list";
import {
  addGeneratedToLibrary,
  moveInLibrary,
  removeFromLibrary,
  type LibraryApi,
} from "@/utils/assets/library-actions";
import { createPagedList } from "@/utils/assets/paged-list";

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

const asset = (id: string): AssetDto => ({
  id,
  url: `/files/${id}`,
  kind: "image",
  mimeType: "image/png",
  byteSize: 1,
  width: 10,
  height: 10,
  durationMs: null,
  fileName: `${id}.png`,
});

const libraryItem = (id: string, folderId: string | null = null): LibraryItemDto => ({
  id,
  asset: asset(`a${id}`),
  folderId,
  createdAt: "2026-10-16T00:00:00Z",
});

const generated = (assetId: string, libraryItemId: string | null = null): GeneratedItemDto => ({
  asset: asset(assetId),
  prompt: "雪地",
  modelLabel: "Seedream",
  canvas: null,
  createdAt: "2026-10-16T00:00:00Z",
  libraryItemId,
});

/** 用固定内容预热一个分页列表 */
async function loaded<T>(items: T[], reload?: () => void) {
  const list = createPagedList<T>(async () => {
    reload?.();
    return { items, nextCursor: "" };
  });
  list.load();
  await tick();
  return list;
}

/** 假接口：默认全成功，可按需让某个方法失败 */
function fakeApi(overrides: Partial<LibraryApi> = {}) {
  const calls: string[] = [];
  const api: LibraryApi = {
    addToLibrary: async (assetId, folderId) => {
      calls.push(`add:${assetId}:${folderId}`);
      return libraryItem("new", folderId);
    },
    moveLibraryItem: async (id, folderId) => {
      calls.push(`move:${id}:${folderId}`);
    },
    removeLibraryItem: async (id) => {
      calls.push(`remove:${id}`);
    },
    ...overrides,
  };
  return { api, calls };
}

const notFound = () => Object.assign(new Error("不存在"), { status: 404 });

describe("addGeneratedToLibrary：生成历史加入资产库", () => {
  test("成功后该素材的 libraryItemId 被写上，卡片会显示「已在资产库」", async () => {
    const store = await loaded([generated("1"), generated("2")]);
    const { api, calls } = fakeApi();
    const result = await addGeneratedToLibrary(store, generated("2"), "f1", api);
    expect(calls).toEqual(["add:2:f1"]);
    expect(result?.id).toBe("new");
    const items = store.getState().items;
    expect(items[0]!.libraryItemId).toBeNull();
    expect(items[1]!.libraryItemId).toBe("new");
  });

  test("加到根目录时 folderId 传 null", async () => {
    const store = await loaded([generated("1")]);
    const { api, calls } = fakeApi();
    await addGeneratedToLibrary(store, generated("1"), null, api);
    expect(calls).toEqual(["add:1:null"]);
  });

  test("失败时列表不变，返回 null", async () => {
    const store = await loaded([generated("1")]);
    const { api } = fakeApi({
      addToLibrary: async () => {
        throw new Error("boom");
      },
    });
    expect(await addGeneratedToLibrary(store, generated("1"), null, api)).toBeNull();
    expect(store.getState().items[0]!.libraryItemId).toBeNull();
  });
});

describe("removeFromLibrary：移出资产库（乐观更新）", () => {
  test("请求还没返回，卡片就已经从列表里消失", async () => {
    const store = await loaded([libraryItem("1"), libraryItem("2"), libraryItem("3")]);
    let release: () => void = () => {};
    const { api } = fakeApi({
      removeLibraryItem: () => new Promise<void>((resolve) => (release = resolve)),
    });
    const pending = removeFromLibrary(store, "2", api);
    expect(store.getState().items.map((item) => item.id)).toEqual(["1", "3"]);
    release();
    expect(await pending).toBe(true);
    expect(store.getState().items.map((item) => item.id)).toEqual(["1", "3"]);
  });

  test("失败时回滚到原来的位置", async () => {
    const store = await loaded([libraryItem("1"), libraryItem("2"), libraryItem("3")]);
    const { api } = fakeApi({
      removeLibraryItem: async () => {
        throw new Error("boom");
      },
    });
    expect(await removeFromLibrary(store, "2", api)).toBe(false);
    expect(store.getState().items.map((item) => item.id)).toEqual(["1", "2", "3"]);
  });

  test("回滚时中间插入的新内容不丢", async () => {
    const store = await loaded([libraryItem("1"), libraryItem("2")]);
    let fail: (error: Error) => void = () => {};
    const { api } = fakeApi({
      removeLibraryItem: () => new Promise<void>((_, reject) => (fail = reject)),
    });
    const pending = removeFromLibrary(store, "1", api);
    store.update((items) => [libraryItem("9"), ...items]);
    fail(new Error("boom"));
    await pending;
    expect(
      store
        .getState()
        .items.map((item) => item.id)
        .sort(),
    ).toEqual(["1", "2", "9"]);
  });

  test("404（别处已删）时回滚并重新拉取当前列表", async () => {
    let loads = 0;
    const store = await loaded([libraryItem("1")], () => loads++);
    const { api } = fakeApi({
      removeLibraryItem: async () => {
        throw notFound();
      },
    });
    await removeFromLibrary(store, "1", api);
    await tick();
    expect(loads).toBe(2);
  });
});

describe("moveInLibrary：移动到文件夹", () => {
  test("在文件夹或根目录视图里，移走的素材立刻从当前列表消失", async () => {
    const store = await loaded([libraryItem("1"), libraryItem("2")]);
    const { api, calls } = fakeApi();
    const ok = await moveInLibrary(store, "1", "f2", { folder: null, search: false }, api);
    expect(ok).toBe(true);
    expect(calls).toEqual(["move:1:f2"]);
    expect(store.getState().items.map((item) => item.id)).toEqual(["2"]);
  });

  test("目标就是当前文件夹时不发请求也不改列表", async () => {
    const store = await loaded([libraryItem("1", "f1")]);
    const { api, calls } = fakeApi();
    await moveInLibrary(store, "1", "f1", { folder: "f1", search: false }, api);
    expect(calls).toEqual([]);
    expect(store.getState().items).toHaveLength(1);
  });

  test("搜索视图是跨文件夹平铺的：素材留在列表里，只改归属", async () => {
    const store = await loaded([libraryItem("1", null)]);
    const { api } = fakeApi();
    await moveInLibrary(store, "1", "f2", { folder: null, search: true }, api);
    expect(store.getState().items).toHaveLength(1);
    expect(store.getState().items[0]!.folderId).toBe("f2");
  });

  test("失败时回滚", async () => {
    const store = await loaded([libraryItem("1"), libraryItem("2")]);
    const { api } = fakeApi({
      moveLibraryItem: async () => {
        throw new Error("boom");
      },
    });
    const ok = await moveInLibrary(store, "1", "f2", { folder: null, search: false }, api);
    expect(ok).toBe(false);
    expect(store.getState().items.map((item) => item.id)).toEqual(["1", "2"]);
  });
});

describe("文件夹列表的本地更新", () => {
  const folder = (id: string, name: string, itemCount = 0): FolderDto => ({
    id,
    name,
    itemCount,
    createdAt: "2026-10-16T00:00:00Z",
  });

  test("新建追加到末尾，重命名只改名字，删除移除对应项", () => {
    let list = [folder("1", "角色")];
    list = addFolder(list, folder("2", "场景"));
    expect(list.map((item) => item.name)).toEqual(["角色", "场景"]);
    list = renameFolder(list, "1", "主角");
    expect(list.map((item) => item.name)).toEqual(["主角", "场景"]);
    list = removeFolder(list, "1");
    expect(list.map((item) => item.id)).toEqual(["2"]);
  });

  test("素材数加减，最低到 0", () => {
    const list = [folder("1", "角色", 1)];
    expect(adjustFolderCount(list, "1", 2)[0]!.itemCount).toBe(3);
    expect(adjustFolderCount(list, "1", -5)[0]!.itemCount).toBe(0);
    expect(adjustFolderCount(list, null, 1)).toEqual(list);
  });
});
