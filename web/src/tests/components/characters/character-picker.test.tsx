import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { AssetDto } from "@/api/asset/type";
import type { CharacterDto } from "@/api/character/type";
import { CharacterCard } from "@/components/characters/character-card";
import { CharacterPickTile } from "@/components/characters/character-picker-dialog";

const asset = (id: string, kind: AssetDto["kind"] = "image"): AssetDto => ({
  id,
  kind,
  url: `/f/${id}`,
  mimeType: "",
  byteSize: 1,
  width: null,
  height: null,
  durationMs: 5000,
  fileName: `file-${id}`,
});

const card = (patch: Partial<CharacterDto> = {}): CharacterDto => ({
  id: "c1",
  name: "林晚",
  description: "",
  avatar: asset("1"),
  extras: [asset("2"), asset("3")],
  voice: asset("9", "audio"),
  createdAt: "",
  updatedAt: "",
  ...patch,
});

const noop = () => {};

describe("CharacterPickTile：双击选择框里的卡片", () => {
  const render = (props: { selected?: boolean; placed?: boolean; card?: CharacterDto }) =>
    renderToStaticMarkup(
      <CharacterPickTile
        card={props.card ?? card()}
        selected={props.selected ?? false}
        placed={props.placed ?? false}
        onToggle={noop}
      />,
    );

  test("名称、有声音、图数（主图 + 补充图）", () => {
    const out = render({});
    expect(out).toContain("林晚");
    expect(out).toContain("有声音");
    expect(out).toContain("3 张图");
  });

  test("没有声音显示「无声音」", () => {
    expect(render({ card: card({ voice: null }) })).toContain("无声音");
  });

  test("已在当前画布上的标「画布中已有」，没有就不标", () => {
    expect(render({ placed: true })).toContain("画布中已有");
    expect(render({ placed: false })).not.toContain("画布中已有");
  });

  test("勾选状态走 aria-pressed / data-selected", () => {
    const on = render({ selected: true });
    expect(on).toContain('aria-pressed="true"');
    expect(on).toContain("data-selected");
    expect(render({ selected: false })).toContain('aria-pressed="false"');
  });

  test("主图素材已不可用：仍能渲染（人形占位），可以选它或点进去重选", () => {
    expect(render({ card: card({ avatar: null }) })).toContain("林晚");
  });
});

describe("CharacterCard：资产页网格里的卡片", () => {
  const render = (c: CharacterDto) =>
    renderToStaticMarkup(<CharacterCard card={c} onEdit={noop} onDelete={noop} />);

  test("名称、试听条（带时长）、补充图叠放", () => {
    const out = render(card());
    expect(out).toContain("林晚");
    expect(out).toContain('aria-label="试听"');
    expect(out).toContain("0:05");
    expect(out).toContain('data-slot="extra-stack"');
    expect(out).toContain("2 张补充图");
  });

  test("没有声音显示「未绑定声音」，没有补充图不叠放", () => {
    const out = render(card({ voice: null, extras: [] }));
    expect(out).toContain("未绑定声音");
    expect(out).not.toContain('data-slot="extra-stack"');
    expect(out).not.toContain('aria-label="试听"');
  });

  test("补充图叠放最多 3 张", () => {
    const out = render(card({ extras: [asset("2"), asset("3"), asset("4"), asset("5")] }));
    expect(out.match(/ring-background/g)).toHaveLength(3);
    expect(out).toContain("4 张补充图");
  });

  test("点封面编辑：封面按钮的名字带角色名", () => {
    expect(render(card())).toContain('aria-label="编辑 林晚"');
  });
});
