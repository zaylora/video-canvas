import { useState } from "react";
import { Loader2 } from "lucide-react";
import { toast } from "sonner";

import type { AssetDto } from "@/api/asset/type";
import { createCharacter, updateCharacter } from "@/api/character";
import type { CharacterDto } from "@/api/character/type";
import { AssetPickerDialog } from "@/components/assets/asset-picker-dialog";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useCharactersStore } from "@/store/characters";
import {
  EXTRA_MAX,
  addExtras,
  assetReason,
  emptyForm,
  formFromCharacter,
  mapSaveError,
  pickAvatar,
  removeExtra,
  setMain,
  toInput,
  validateForm,
  type EditorErrors,
  type EditorForm,
  type PickMode,
} from "@/utils/characters/editor-form";

import { CharacterEditorFields } from "./character-editor-fields";

/** 选素材弹框的标题 */
const PICK_TITLE: Record<PickMode, string> = {
  avatar: "选择形象主图",
  extra: "添加补充图",
  voice: "选择声音",
};

/**
 * 角色卡新建 / 编辑弹框（资产页与画布选择框共用）。左图右文，选素材复用 AssetPickerDialog 并按种类过滤
 * （主图 / 补充图只列图片，声音只列音频）。保存中按钮转圈、弹框不可关闭；失败时已填内容保留：
 * 重名显示在名称框下，数量上限 / 素材不可用 / 参数不合法在底部横幅，角色卡已被删则提示并关闭。
 * 保存成功后写进角色卡缓存，引用它的画布节点自动刷新。每次打开都从初始状态开始。
 * @param open 是否打开
 * @param character 要编辑的角色卡；null 是新建
 * @param onClose 关闭（取消、保存成功、角色卡已被删）
 * @param onSaved 保存成功：拿到最新角色卡（画布选择框据此自动选中新建的卡）
 */
export function CharacterEditorDialog({
  open,
  character,
  onClose,
  onSaved,
}: {
  open: boolean;
  character: CharacterDto | null;
  onClose: () => void;
  onSaved?: (card: CharacterDto) => void;
}) {
  const [saving, setSaving] = useState(false);
  const title = character ? "编辑角色卡" : "新建角色卡";
  /** 保存中不可关闭；内容打开时才挂载，状态每次从头开始 */
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && !saving) onClose();
      }}
    >
      <DialogContent
        showCloseButton={!saving}
        data-slot="character-editor"
        className="max-h-[calc(100dvh-2rem)] gap-0 overflow-y-auto p-0 sm:max-w-3xl"
      >
        <DialogHeader className="px-5 pt-4 pb-3">
          <DialogTitle className="text-base">{title}</DialogTitle>
          <DialogDescription className="sr-only">
            设置角色的名称、形象图、声音和设定
          </DialogDescription>
        </DialogHeader>
        {open && (
          <EditorBody
            character={character}
            saving={saving}
            setSaving={setSaving}
            onClose={onClose}
            onSaved={onSaved}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function EditorBody({
  character,
  saving,
  setSaving,
  onClose,
  onSaved,
}: {
  character: CharacterDto | null;
  saving: boolean;
  setSaving: (saving: boolean) => void;
  onClose: () => void;
  onSaved?: (card: CharacterDto) => void;
}) {
  const [form, setForm] = useState<EditorForm>(() =>
    character ? formFromCharacter(character) : emptyForm(),
  );
  const [errors, setErrors] = useState<EditorErrors>({});
  const [banner, setBanner] = useState<string | null>(null);
  const [pick, setPick] = useState<PickMode | null>(null);

  /** 改了哪个字段就清掉那个字段的错误，其余保留 */
  const edit = (patch: Partial<EditorForm>, clear: keyof EditorErrors | null = null) => {
    setForm((prev) => ({ ...prev, ...patch }));
    if (clear) setErrors((prev) => ({ ...prev, [clear]: undefined }));
    setBanner(null);
  };

  const confirmPick = (assets: AssetDto[]) => {
    if (pick === "avatar" && assets[0]) {
      setForm((prev) => pickAvatar(prev, assets[0]));
      setErrors((prev) => ({ ...prev, avatar: undefined }));
    } else if (pick === "extra") {
      setForm((prev) => addExtras(prev, assets));
    } else if (pick === "voice" && assets[0]) {
      setForm((prev) => ({ ...prev, voice: assets[0] }));
    }
    setBanner(null);
  };

  const save = async () => {
    const found = validateForm(form);
    setErrors(found);
    if (Object.keys(found).length > 0) return;
    setBanner(null);
    setSaving(true);
    try {
      const input = toInput(form);
      const saved = character
        ? await updateCharacter(character.id, input)
        : await createCharacter(input);
      useCharactersStore.getState().upsert(saved);
      toast.success(character ? "已保存，引用它的画布节点会同步更新" : "已创建角色卡");
      onSaved?.(saved);
      onClose();
    } catch (error) {
      const failure = mapSaveError(error);
      if (failure.kind === "field") setErrors({ [failure.field]: failure.message });
      else if (failure.kind === "banner") setBanner(failure.message);
      else {
        // 编辑时角色卡已被别处删除：缓存标成查不到，关弹框
        if (character) useCharactersStore.getState().remove(character.id);
        toast.error(failure.message);
        onClose();
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <>
      <div className="px-5 pb-4">
        <CharacterEditorFields
          form={form}
          errors={errors}
          disabled={saving}
          onPick={setPick}
          onSetMain={(index) => edit(setMain(form, index))}
          onRemoveExtra={(index) => edit(removeExtra(form, index))}
          onClearVoice={() => edit({ voice: null })}
          onNameChange={(name) => edit({ name }, "name")}
          onDescriptionChange={(description) => edit({ description }, "description")}
        />
      </div>

      {banner && (
        <div
          role="alert"
          data-slot="save-banner"
          className="bg-destructive/10 text-destructive mx-5 mb-3 rounded-lg px-3 py-2 text-[13px]"
        >
          {banner}
        </div>
      )}

      <DialogFooter className="border-t px-5 py-3 sm:items-center">
        <Button variant="outline" disabled={saving} onClick={onClose}>
          取消
        </Button>
        <Button disabled={saving} onClick={() => void save()}>
          {saving && <Loader2 className="animate-spin motion-reduce:animate-none" />}
          {saving ? "保存中…" : character ? "保存" : "创建"}
        </Button>
      </DialogFooter>

      {/* 放在编辑弹框里面：嵌套弹框才能共用焦点与 Esc 的层级，Esc 先关选素材这层 */}
      <AssetPickerDialog
        open={pick !== null}
        onOpenChange={(next) => !next && setPick(null)}
        kind={pick === "voice" ? "audio" : "image"}
        limit={pick === "extra" ? Math.max(1, EXTRA_MAX - form.extras.length) : 1}
        reasonOf={(asset) => (pick && pick !== "avatar" ? assetReason(form, pick, asset) : null)}
        title={pick ? PICK_TITLE[pick] : undefined}
        confirmLabel="确定"
        onConfirm={confirmPick}
      />
    </>
  );
}
