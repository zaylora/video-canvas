import { useCallback } from "react";
import { toast } from "sonner";

import { uploadAsset } from "@/api/asset";
import type { ModelInfo, RefKind } from "@/api/model/type";
import { useComposerStore } from "@/store/composer";
import { useModelsStore } from "@/store/models";
import {
  acceptableRefKinds,
  currentOp,
  opToAcceptSource,
  refKindOfFile,
  refUploadError,
} from "@/utils/tasks/capabilities";

/** 正在上传或上传失败的文件：重试要用到原文件，不进 store（File 不可序列化） */
const pendingFiles = new Map<string, File>();

/** 本地预览地址：上传完成换成素材地址后要还回去 */
const previewUrls = new Map<string, string>();

const releasePreview = (id: string) => {
  const url = previewUrls.get(id);
  if (url) URL.revokeObjectURL(url);
  previewUrls.delete(id);
};

/** 输入卡片此刻选中的模型：记住的那个，已下线或没记过取清单第一个（与输入卡片的取法一致） */
export function currentComposerModel(): ModelInfo | undefined {
  const { mode, modelByMode } = useComposerStore.getState();
  const models = useModelsStore.getState().byKind[mode]?.models ?? [];
  return models.find((item) => item.key === modelByMode[mode]) ?? models[0];
}

/**
 * 加进一种参考素材后，生成方式不收它就切到收它的方式（优先全能参考）：
 * 与画布里「文生视频」节点连上图片线、音频线时自动变成全能参考是同一条规则（opToAcceptSource）。
 */
function switchOpToAccept(model: ModelInfo, kind: RefKind) {
  const store = useComposerStore.getState();
  const params = store.paramsByModel[model.key] ?? {};
  const hasImage = store.refs.some((ref) => ref.kind === "image");
  const op = currentOp(model.capabilities, params, hasImage);
  const next = opToAcceptSource(model.capabilities, op, kind);
  if (next) store.setParam(model.key, "op", next);
}

/** 这种素材已经放进输入卡片几个 */
const usedOf = (kind: RefKind) =>
  useComposerStore.getState().refs.filter((ref) => ref.kind === kind).length;

/** 上传一个文件并把结果写回这份参考；失败的提示由请求层统一弹 */
async function upload(id: string, file: File) {
  const { updateRef } = useComposerStore.getState();
  updateRef(id, { status: "uploading" });
  try {
    const asset = await uploadAsset(file);
    pendingFiles.delete(id);
    releasePreview(id);
    updateRef(id, { assetId: asset.id, url: asset.url, status: "done" });
  } catch {
    updateRef(id, { status: "error" });
  }
}

/**
 * 把选中 / 粘贴 / 拖入的文件加成参考素材并上传。校验与画布的参考素材卡是同一套（refUploadError）：
 * 种类认不认、模型收不收、这种素材满没满、单个文件大不大；通过的立即显示缩略图并上传。
 * @param model 当前模型；没有可用模型时什么都不加
 * @param files 文件
 */
export function addReferenceFiles(model: ModelInfo | undefined, files: File[]) {
  if (!model) return;
  const caps = model.capabilities;
  const accepted = acceptableRefKinds(caps);
  for (const file of files) {
    const kind = refKindOfFile(file);
    if (!kind) {
      toast.error("参考只支持图片、视频和音频");
      continue;
    }
    const reason = accepted.includes(kind)
      ? refUploadError(caps.refs[kind], kind, file, usedOf(kind))
      : refUploadError(undefined, kind, file, 0);
    if (reason) {
      toast.error(reason);
      continue;
    }
    const id = crypto.randomUUID();
    const url = kind === "audio" ? undefined : URL.createObjectURL(file);
    if (url) previewUrls.set(id, url);
    pendingFiles.set(id, file);
    useComposerStore.getState().addRef({ id, kind, name: file.name, url, status: "uploading" });
    switchOpToAccept(model, kind);
    void upload(id, file);
  }
}

/**
 * 把已有素材（对话里的结果、重新编辑还原的参考）加为参考，不重新上传。
 * 当前模型不收这种素材或已满时仍然加进去（用户可能马上换模型），但用 toast 说明原因，
 * 超出的会在输入卡片里标黄，发送前会提示移除。
 * @param model 当前模型
 * @param asset 素材
 */
export function addReferenceAsset(
  model: ModelInfo | undefined,
  asset: { kind: RefKind; assetId: string; url?: string; name: string },
) {
  const store = useComposerStore.getState();
  if (store.refs.some((ref) => ref.assetId === asset.assetId && ref.kind === asset.kind)) {
    toast.info("已经在参考里了");
    return;
  }
  const reason = model
    ? refUploadError(
        model.capabilities.refs[asset.kind],
        asset.kind,
        { size: 0 },
        usedOf(asset.kind),
      )
    : "暂无可用模型";
  store.addRef({ id: crypto.randomUUID(), status: "done", ...asset });
  if (reason) {
    toast.warning(`已加入参考，但${reason}`);
    return;
  }
  if (model) switchOpToAccept(model, asset.kind);
  toast.success("已加入输入卡片的参考");
}

/**
 * 输入卡片的参考素材：选文件 / 粘贴 / 拖入后立即显示缩略图并上传，上传完成换成素材。
 * @param model 当前模型
 * @returns addFiles 添加文件；retry 重试上传失败的；remove 移除
 */
export function useComposerRefs(model: ModelInfo | undefined) {
  const addFiles = useCallback((files: File[]) => addReferenceFiles(model, files), [model]);

  const retry = useCallback((id: string) => {
    const file = pendingFiles.get(id);
    if (file) void upload(id, file);
  }, []);

  const remove = useCallback((id: string) => {
    pendingFiles.delete(id);
    releasePreview(id);
    useComposerStore.getState().removeRef(id);
  }, []);

  return { addFiles, retry, remove };
}
