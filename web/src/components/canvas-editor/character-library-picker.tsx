import type { AssetDto } from "@/api/asset/type";
import { AssetPickerDialog } from "@/components/assets/asset-picker-dialog";
import type { LibraryRequest } from "@/hooks/use-character-media";
import type { CanvasNodeData } from "@/types";
import { MAX_EXTRAS } from "@/utils/characters/node-character";

/** 素材库选择框的标题 */
const TITLE = { main: "选择形象主图", extra: "选择补充图", voice: "选择音色音频" } as const;

/**
 * 角色卡用的素材库选择框：按用途锁定种类（主图、补充图只列图片，音色只列音频），
 * 补充图可多选、最多补满 4 张，已是主图 / 已添加的素材置灰并写原因（不隐藏）。
 * 选完交给 onConfirm，由调用方用 applyPickedAssets 写回节点。
 * @param request 要选什么；null 是关着
 * @param data 当前节点数据，用来判断哪些素材已被用掉
 * @param onClose 关闭
 * @param onConfirm 确认：选中的素材和用途
 */
export function LibraryPickerForCharacter({
  request,
  data,
  onClose,
  onConfirm,
}: {
  request: LibraryRequest;
  data: CanvasNodeData;
  onClose: () => void;
  onConfirm: (assets: AssetDto[], purpose: "main" | "extra" | "voice") => void;
}) {
  const extras = data.character?.extras ?? [];
  const purpose = request?.purpose;
  return (
    <AssetPickerDialog
      open={request !== null}
      onOpenChange={(open) => !open && onClose()}
      kind={request?.kind}
      limit={purpose === "extra" ? Math.max(1, MAX_EXTRAS - extras.length) : 1}
      reasonOf={(asset) => {
        if (purpose === "extra") {
          if (asset.id === data.assetId) return "已是主图";
          if (extras.some((extra) => extra.assetId === asset.id)) return "已添加";
        }
        return null;
      }}
      title={purpose ? TITLE[purpose] : undefined}
      confirmLabel="确定"
      onConfirm={(assets) => purpose && onConfirm(assets, purpose)}
    />
  );
}
