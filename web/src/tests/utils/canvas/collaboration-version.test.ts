import { expect, test } from "bun:test";
import {
  compareCanvasVersions,
  nextCanvasVersion,
  parseCanvasVersions,
  stringifyCanvasVersions,
} from "@/utils/canvas/collaboration-version";

test("协作版本往返 JSON 时保留相邻的大整数和字段起点", () => {
  const raw =
    '{"epoch":9007199254740993,"baseRevision":9007199254740992,"fieldBases":{"data.prompt":9007199254740993},"label":"9007199254740993"}';
  const value = parseCanvasVersions(raw) as { epoch: string; baseRevision: string };
  expect(value.epoch).toBe("9007199254740993");
  expect(nextCanvasVersion(value.baseRevision)).toBe(value.epoch);
  expect(compareCanvasVersions("9999999999999999", "10000000000000000")).toBeLessThan(0);
  expect(stringifyCanvasVersions(value)).toBe(raw);
});

test("请求只编码协议版本，保留用户参数和字段值里的同名字符串", () => {
  const version = "9007199254740993";
  const body = {
    epoch: version,
    collaboration: { revision: version },
    changes: [
      {
        fieldBases: { "data.prompt": version },
        fields: { "data.custom": { revision: version, fieldBases: { epoch: version } } },
      },
    ],
    input: { epoch: version },
  };
  const raw = stringifyCanvasVersions(body);
  expect(raw).toBe(
    '{"epoch":9007199254740993,"collaboration":{"revision":9007199254740993},"changes":[{"fieldBases":{"data.prompt":9007199254740993},"fields":{"data.custom":{"revision":"9007199254740993","fieldBases":{"epoch":"9007199254740993"}}}}],"input":{"epoch":"9007199254740993"}}',
  );
});
