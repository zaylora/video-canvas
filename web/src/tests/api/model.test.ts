import { afterEach, beforeEach, describe, expect, spyOn, test, type Mock } from "bun:test";

import { getModels } from "@/api/model";
import service from "@/utils/requests/service";

describe("getModels：模型没有配置能力时的兜底", () => {
  let get: Mock<typeof service.get>;

  beforeEach(() => {
    get = spyOn(service, "get") as unknown as Mock<typeof service.get>;
  });
  afterEach(() => get.mockRestore());

  test("没有提示词字数上限时兜底 20000，与新建模型的预填值一致", async () => {
    get.mockResolvedValue([{ key: "m", name: "M", kind: "image" }] as never);
    const [m] = await getModels("image");
    expect(m.capabilities.prompt).toEqual({ max_length: 20000 });
  });

  test("模型自己配置了上限就用它的，不被兜底覆盖", async () => {
    get.mockResolvedValue([
      { key: "m", name: "M", kind: "image", capabilities: { prompt: { max_length: 1500 } } },
    ] as never);
    const [m] = await getModels("image");
    expect(m.capabilities.prompt).toEqual({ max_length: 1500 });
  });
});
