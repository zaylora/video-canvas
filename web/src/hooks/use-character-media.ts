import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { uploadAsset } from "@/api/asset";
import type { AssetDto } from "@/api/asset/type";
import { takeCharacterFile } from "@/utils/characters/node-character";

/** 角色卡一次选 / 传素材的用途：主图、补充图用图片，音色用音频 */
export type CharacterMediaKind = "image" | "audio";

/** 素材库选择框此刻要选什么；null 是关着 */
export type LibraryRequest = {
  kind: CharacterMediaKind;
  purpose: "main" | "extra" | "voice";
} | null;

/**
 * 角色卡节点和详情页共用的「补素材」能力：上传本地文件、打开素材库选择框。
 * 上传先过 takeCharacterFile（只收对的种类、卡大小），传完把素材交给 onAsset；
 * 上传失败的全局提示由请求层统一弹，这里只记失败和结束，不重复弹。
 * 素材库选择框由调用方渲染（nested dialog 要放在各自的弹框里），这里只管它开不开、选什么。
 * @param onAsset 拿到素材后的处理：写进节点
 */
export function useCharacterMedia(
  onAsset: (asset: AssetDto, purpose: "main" | "extra" | "voice") => void,
) {
  const [uploading, setUploading] = useState<"main" | "extra" | "voice" | null>(null);
  const [library, setLibrary] = useState<LibraryRequest>(null);
  const latest = useRef(onAsset);
  useEffect(() => {
    latest.current = onAsset;
  });

  /** 选完文件：校验、上传、交出素材 */
  const upload = useCallback(
    async (file: File, kind: CharacterMediaKind, purpose: "main" | "extra" | "voice") => {
      const check = takeCharacterFile(file, kind);
      if (!check.ok) {
        toast.error(check.reason);
        return;
      }
      setUploading(purpose);
      try {
        latest.current(await uploadAsset(file), purpose);
      } catch {
        // 失败提示由请求层统一弹出；这里只需要结束上传中的状态
      } finally {
        setUploading(null);
      }
    },
    [],
  );

  /**
   * 打开系统文件框：每次临时建一个 input，不挂进页面（不用持有元素的 ref，也就没有渲染期读 ref 的问题），
   * 选完文件再校验种类和大小后上传。
   */
  const pickFile = useCallback(
    (kind: CharacterMediaKind, purpose: "main" | "extra" | "voice") => {
      const input = document.createElement("input");
      input.type = "file";
      input.accept = kind === "image" ? "image/*" : "audio/*";
      input.onchange = () => {
        const file = input.files?.[0];
        if (file) void upload(file, kind, purpose);
      };
      input.click();
    },
    [upload],
  );

  const openLibrary = useCallback(
    (kind: CharacterMediaKind, purpose: "main" | "extra" | "voice") =>
      setLibrary({ kind, purpose }),
    [],
  );
  const closeLibrary = useCallback(() => setLibrary(null), []);

  return { pickFile, uploading, library, openLibrary, closeLibrary };
}
