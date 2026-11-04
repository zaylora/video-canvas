import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  spyOn,
  test,
} from "bun:test";

import { setSkillDramaSteps } from "@/api/admin/agent-skill";
import { adminAgentSkillEndpoints as ep } from "@/api/admin/agent-skill/endpoints";
import type { DramaStepKey, SkillItem } from "@/api/admin/agent-skill/type.d";
import { installDom, type DomEnv } from "@/tests/helpers/dom";
import service from "@/utils/requests/service";
import type { DramaStepsField as DramaStepsFieldType } from "@/pages/admin/agent-skills/drama-steps-field";

/**
 * 后台技能页的「短剧适用环节」：多选四个环节，点保存调 PUT /admin/agent/skills/:name/drama-steps。
 */

const item = (patch: Partial<SkillItem> = {}): SkillItem => ({
  name: "cross-episode-continuity",
  title: "跨集连续性",
  description: "保持角色一致",
  source: "imported",
  readonly: false,
  enabled: true,
  active_version: 1,
  latest_version: 1,
  pending_version: false,
  version_count: 1,
  file_count: 1,
  total_bytes: 1,
  has_scripts: false,
  unsupported_fields: [],
  drama_steps: [],
  updated_at: "2026-01-01T00:00:00Z",
  ...patch,
});

describe("短剧适用环节：接口", () => {
  let put: ReturnType<typeof spyOn>;
  beforeEach(() => {
    put = spyOn(service, "put");
  });
  afterEach(() => put.mockRestore());

  test("路径与请求体：PUT /admin/agent/skills/:name/drama-steps，{steps}", async () => {
    expect(ep.dramaSteps("a b")).toBe("/admin/agent/skills/a%20b/drama-steps");
    put.mockResolvedValue(item({ drama_steps: ["assets", "video"] }));
    const next = await setSkillDramaSteps("cross-episode-continuity", ["assets", "video"]);
    expect(put.mock.calls[0][0]).toBe("/admin/agent/skills/cross-episode-continuity/drama-steps");
    expect(put.mock.calls[0][1]).toEqual({ steps: ["assets", "video"] });
    expect(next.drama_steps).toEqual(["assets", "video"]);
  });

  test("后端没带 drama_steps（老数据 / null）时补成空数组", async () => {
    put.mockResolvedValue({ ...item(), drama_steps: null });
    expect((await setSkillDramaSteps("x", [])).drama_steps).toEqual([]);
  });
});

describe("短剧适用环节：多选控件", () => {
  let dom: DomEnv;
  let DramaStepsField: typeof DramaStepsFieldType;
  beforeAll(async () => {
    dom = await installDom();
    DramaStepsField = (await import("@/pages/admin/agent-skills/drama-steps-field"))
      .DramaStepsField;
  });
  afterAll(() => dom.restore());

  const chips = (c: HTMLElement) => [...c.querySelectorAll("button[aria-pressed]")];
  const byText = (c: HTMLElement, text: string) =>
    [...c.querySelectorAll("button")].find((b) => (b.textContent ?? "").trim() === text);

  test("四个环节：剧本改编 / 资产拆解 / 资产图提示词 / 视频提示词，已标的高亮；没改动保存禁用", async () => {
    const m = await dom.mount(
      <DramaStepsField item={item({ drama_steps: ["video"] })} onSave={async () => null} />,
    );
    expect(chips(m.container).map((b) => b.textContent)).toEqual([
      "剧本改编",
      "资产拆解",
      "资产图提示词",
      "视频提示词",
    ]);
    expect(chips(m.container).map((b) => b.getAttribute("aria-pressed"))).toEqual([
      "false",
      "false",
      "false",
      "true",
    ]);
    expect((byText(m.container, "保存") as HTMLButtonElement).disabled).toBe(true);
    await m.unmount();
  });

  test("多选后点保存：按固定顺序提交选中的环节；保存成功（列表里的技能更新）后回到「没改动」", async () => {
    const saved: DramaStepKey[][] = [];
    const { useState } = await import("react");
    function Host() {
      const [current, setCurrent] = useState(item());
      return (
        <DramaStepsField
          item={current}
          onSave={async (steps) => {
            saved.push(steps);
            const next = item({ drama_steps: steps });
            setCurrent(next);
            return next;
          }}
        />
      );
    }
    const m = await dom.mount(<Host />);
    await dom.click(byText(m.container, "视频提示词"));
    await dom.click(byText(m.container, "资产拆解"));
    expect((byText(m.container, "保存") as HTMLButtonElement).disabled).toBe(false);
    await dom.click(byText(m.container, "保存"));
    await dom.flush();
    expect(saved).toEqual([["assets", "video"]]);
    expect((byText(m.container, "保存") as HTMLButtonElement).disabled).toBe(true);
    await m.unmount();
  });

  test("可以取消全部（空数组）", async () => {
    const saved: DramaStepKey[][] = [];
    const m = await dom.mount(
      <DramaStepsField
        item={item({ drama_steps: ["adapt"] })}
        onSave={async (steps) => {
          saved.push(steps);
          return item({ drama_steps: steps });
        }}
      />,
    );
    await dom.click(byText(m.container, "剧本改编"));
    await dom.click(byText(m.container, "保存"));
    await dom.flush();
    expect(saved).toEqual([[]]);
    await m.unmount();
  });

  test("保存失败（返回 null）：选择保留，可以再试", async () => {
    const m = await dom.mount(<DramaStepsField item={item()} onSave={async () => null} />);
    await dom.click(byText(m.container, "视频提示词"));
    await dom.click(byText(m.container, "保存"));
    await dom.flush();
    expect(byText(m.container, "视频提示词")?.getAttribute("aria-pressed")).toBe("true");
    expect((byText(m.container, "保存") as HTMLButtonElement).disabled).toBe(false);
    await m.unmount();
  });

  test("内置技能只读：环节禁用并写明原因，没有保存按钮", async () => {
    const m = await dom.mount(
      <DramaStepsField
        item={item({ readonly: true, source: "builtin" })}
        onSave={async () => null}
      />,
    );
    for (const chip of chips(m.container)) {
      expect((chip as HTMLButtonElement).disabled).toBe(true);
      expect((chip as HTMLButtonElement).title).toBe("内置技能不能标注短剧环节");
    }
    expect(byText(m.container, "保存")).toBeUndefined();
    await m.unmount();
  });
});
