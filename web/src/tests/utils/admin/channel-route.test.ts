import { describe, expect, test } from "bun:test";

import { dialogEntry } from "@/utils/admin/dialog-entry";
import {
  channelsPagePath,
  legacyModelsRedirect,
  parseChannelsQuery,
  pickSelectedChannel,
} from "@/utils/admin/channel-route";

describe("channelsPagePath", () => {
  test("不带参数就是主从页本身", () => {
    expect(channelsPagePath()).toBe("/admin/ai/channels");
    expect(channelsPagePath({})).toBe("/admin/ai/channels");
  });

  test("渠道、模型、试跑、状态都放进查询串，test 用 1", () => {
    expect(channelsPagePath({ channel: "yswg", model: "gpt-image-2", test: true })).toBe(
      "/admin/ai/channels?channel=yswg&model=gpt-image-2&test=1",
    );
    expect(channelsPagePath({ status: "off" })).toBe("/admin/ai/channels?status=off");
  });

  test("特殊字符会被编码，test=false 不出现", () => {
    expect(channelsPagePath({ channel: "a b", model: "x/y", test: false })).toBe(
      "/admin/ai/channels?channel=a+b&model=x%2Fy",
    );
  });
});

describe("parseChannelsQuery", () => {
  test("读出渠道、模型、试跑和状态筛选", () => {
    expect(parseChannelsQuery("?channel=yswg&model=m1&test=1&status=off")).toEqual({
      channel: "yswg",
      model: "m1",
      test: true,
      status: "off",
    });
  });

  test("缺省值：没有渠道和模型为 null，test 为 false，状态为 all", () => {
    expect(parseChannelsQuery("")).toEqual({
      channel: null,
      model: null,
      test: false,
      status: "all",
    });
  });

  test("test 只认 1 / true；旧的健康状态筛选并入启用 / 停用", () => {
    expect(parseChannelsQuery("?test=true").test).toBe(true);
    expect(parseChannelsQuery("?test=0").test).toBe(false);
    expect(parseChannelsQuery("?status=ok").status).toBe("on");
    expect(parseChannelsQuery("?status=warn").status).toBe("on");
    expect(parseChannelsQuery("?status=off").status).toBe("off");
    expect(parseChannelsQuery("?status=whatever").status).toBe("all");
  });

  test("空串参数当没有", () => {
    expect(parseChannelsQuery("?channel=&model=").channel).toBeNull();
    expect(parseChannelsQuery("?channel=&model=").model).toBeNull();
  });
});

describe("pickSelectedChannel", () => {
  const channels = [{ key: "a" }, { key: "b" }];

  test("想选的渠道存在就选它", () => {
    expect(pickSelectedChannel(channels, "b")).toBe("b");
  });

  test("没指定或已不存在（被删了）就选第一个", () => {
    expect(pickSelectedChannel(channels, null)).toBe("a");
    expect(pickSelectedChannel(channels, "gone")).toBe("a");
  });

  test("没有渠道返回 null", () => {
    expect(pickSelectedChannel([], "a")).toBeNull();
  });
});

describe("legacyModelsRedirect", () => {
  test("保留 channel / model / test 查询串", () => {
    expect(legacyModelsRedirect("?channel=c&model=m&test=1", null)).toBe(
      "/admin/ai/channels?channel=c&model=m&test=1",
    );
  });

  test("旧的路由 state（dialogEntry）转成查询参数", () => {
    expect(legacyModelsRedirect("", dialogEntry({ key: "m", test: true }))).toBe(
      "/admin/ai/channels?model=m&test=1",
    );
    expect(legacyModelsRedirect("", dialogEntry({ key: "m" }))).toBe("/admin/ai/channels?model=m");
  });

  test("没有任何线索就回到主从页", () => {
    expect(legacyModelsRedirect("", null)).toBe("/admin/ai/channels");
    expect(legacyModelsRedirect("", dialogEntry({ isNew: true }))).toBe("/admin/ai/channels");
  });

  test("查询串优先于 state", () => {
    expect(legacyModelsRedirect("?model=q", dialogEntry({ key: "s" }))).toBe(
      "/admin/ai/channels?model=q",
    );
  });
});
