import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";

import type { AssetDto } from "@/api/asset/type";
import { CharacterEditorFields } from "@/components/characters/character-editor-fields";
import { emptyForm, type EditorErrors, type EditorForm } from "@/utils/characters/editor-form";

const asset = (id: string, kind: AssetDto["kind"] = "image"): AssetDto => ({
  id,
  kind,
  url: `/f/${id}`,
  mimeType: "",
  byteSize: 1,
  width: null,
  height: null,
  durationMs: 12000,
  fileName: `file-${id}`,
});

const noop = () => {};
const render = (form: EditorForm, errors: EditorErrors = {}, disabled = false) =>
  renderToStaticMarkup(
    <CharacterEditorFields
      form={form}
      errors={errors}
      disabled={disabled}
      onPick={noop}
      onSetMain={noop}
      onRemoveExtra={noop}
      onClearVoice={noop}
      onNameChange={noop}
      onDescriptionChange={noop}
    />,
  );

describe("CharacterEditorFields：编辑弹框表单主体", () => {
  test("新建：主图槽是空的「选择主图」，4 个补充图空位只有第一个可点", () => {
    const out = render(emptyForm());
    expect(out).toContain("选择主图");
    expect(out).toContain("0/4");
    expect(out.match(/data-slot="extra-blank"/g)).toHaveLength(4);
    // 只有紧挨着已有补充图的下一个空位可点，其余禁用
    expect(out.match(/data-slot="extra-blank"[^>]*disabled=""/g)).toHaveLength(3);
    expect(out).toContain("从资产库选择一段音频");
    expect(out).toContain("0/500");
  });

  test("校验错误就地显示：名称错在名称框下并标 aria-invalid，没选主图时主图槽描红", () => {
    const out = render(emptyForm(), { name: "请填写角色名称", avatar: "请选择一张形象主图" });
    expect(out).toContain("请填写角色名称");
    expect(out).toContain("请选择一张形象主图");
    expect(out).toContain('aria-invalid="true"');
    expect(out).toContain("border-destructive");
  });

  test("没有错误时不出现任何提示文案", () => {
    const out = render({ ...emptyForm(), name: "林晚", avatar: asset("1") });
    expect(out).not.toContain('role="alert"');
    expect(out).toContain("更换");
    expect(out).toContain(">主图<");
  });

  test("补充图槽带「设为主图」「移除」，数量计数跟着变", () => {
    const out = render({
      ...emptyForm(),
      avatar: asset("1"),
      extras: [asset("2"), asset("3")],
    });
    expect(out.match(/aria-label="设为主图"/g)).toHaveLength(2);
    expect(out.match(/aria-label="移除"/g)).toHaveLength(2);
    expect(out).toContain("2/4");
    expect(out.match(/data-slot="extra-blank"/g)).toHaveLength(2);
  });

  test("绑定了声音：试听卡片显示文件名、时长、更换与移除", () => {
    const out = render({ ...emptyForm(), voice: asset("9", "audio") });
    expect(out).toContain("file-9");
    expect(out).toContain("0:12");
    expect(out).toContain('aria-label="移除声音"');
    expect(out).not.toContain("从资产库选择一段音频");
  });

  test("保存中：所有操作控件禁用，弹框不让改内容", () => {
    const out = render(
      { ...emptyForm(), avatar: asset("1"), extras: [asset("2")], voice: asset("9", "audio") },
      {},
      true,
    );
    expect(out).not.toMatch(/<button(?![^>]*disabled)[^>]*aria-label="(设为主图|移除|移除声音)"/);
    expect(out).toMatch(/<input[^>]*disabled/);
    expect(out).toMatch(/<textarea[^>]*disabled/);
  });

  test("设定超出 500 字：计数标红，错误显示在设定框下", () => {
    const out = render(
      { ...emptyForm(), description: "字".repeat(501) },
      { description: "角色设定不能超过 500 个字" },
    );
    expect(out).toContain("501/500");
    expect(out).toContain("角色设定不能超过 500 个字");
    expect(out).toContain("text-destructive");
  });
});
