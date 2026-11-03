import { describe, expect, test } from "bun:test";

import type { ModelInfo } from "@/api/model/type";
import { pickImageModel } from "@/utils/drama/image-models";

const model = (key: string): ModelInfo => ({
  key,
  kind: "image",
  label: key,
  pricing: { billing: "per_call", unit: 1 },
  capabilities: {},
});

describe("拆解时默认的图片模型", () => {
  const list = [model("first"), model("second")];

  test("项目记下的模型还在就沿用，不在了或没选过取第一个", () => {
    expect(pickImageModel(list, "second")?.key).toBe("second");
    expect(pickImageModel(list, "gone")?.key).toBe("first");
    expect(pickImageModel(list, "")?.key).toBe("first");
    expect(pickImageModel([], "second")).toBeUndefined();
  });
});
