import { useCallback, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { uploadAsset } from "@/api/asset";
import { isUploadAborted } from "@/utils/asset/direct-upload";
import { clipToLimit, documentCount, imageCount, type TrayItem } from "@/utils/agent/attachments";
import { MAX_ATTACH_DOCUMENTS, pickDocumentFiles } from "@/utils/agent/document-files";
import { pickImageFiles } from "@/utils/agent/image-files";

/**
 * Agent 聊天框的附件托盘：加图或加文档（.txt / .md）时直接上传成素材（不碰画布），托盘里按项记着状态，发送时只带素材 id。
 * 上传中可以取消，失败可以重试，移除会取消上传并回收本地预览。
 */
export function useAgentAttachments() {
  const [items, setItems] = useState<TrayItem[]>([]);
  /** 最新的托盘：上传回调里要读最新状态，不能用闭包里的旧值 */
  const latest = useRef<TrayItem[]>([]);
  /** 失败重试要用原文件，File 不能放进 state */
  const files = useRef(new Map<string, File>());
  const aborts = useRef(new Map<string, AbortController>());
  /** 给没有文件名的图片（粘贴来的）起「图片 N」 */
  const counter = useRef(0);

  const commit = useCallback((next: TrayItem[]) => {
    latest.current = next;
    setItems(next);
  }, []);
  const patch = useCallback(
    (id: string, part: Partial<TrayItem>) =>
      commit(latest.current.map((i) => (i.id === id ? { ...i, ...part } : i))),
    [commit],
  );

  const release = useCallback((item: TrayItem) => {
    aborts.current.get(item.id)?.abort();
    aborts.current.delete(item.id);
    files.current.delete(item.id);
    if (item.previewUrl) URL.revokeObjectURL(item.previewUrl);
  }, []);

  const upload = useCallback(
    async (id: string, file: File) => {
      const ctl = new AbortController();
      aborts.current.set(id, ctl);
      patch(id, { status: "uploading", progress: 0, error: undefined });
      try {
        const asset = await uploadAsset(file, {
          signal: ctl.signal,
          onProgress: (progress) => patch(id, { progress }),
        });
        files.current.delete(id);
        patch(id, {
          status: "done",
          progress: 100,
          asset: {
            id: asset.id,
            url: asset.url,
            width: asset.width,
            height: asset.height,
            mimeType: asset.mimeType,
          },
        });
      } catch (e) {
        // 被移除时取消上传：不是失败，条目已经不在托盘里了
        if (isUploadAborted(e)) return;
        patch(id, { status: "error", error: e instanceof Error ? e.message : "上传失败" });
      } finally {
        aborts.current.delete(id);
      }
    },
    [patch],
  );

  /** 加图（粘贴、拖入、＋ 菜单共用）：只收图片，一条消息最多 9 张，多出的和非图片都提示 */
  const add = useCallback(
    (picked: ArrayLike<File> | null | undefined) => {
      const { images, skipped, clipped } = pickImageFiles(picked);
      const room = clipToLimit(imageCount(latest.current), images.length);
      if (skipped > 0) toast.warning("只能添加图片，其他文件已忽略");
      const tooMany = clipped + room.clipped;
      if (tooMany > 0) toast.warning(`一条消息最多 9 张，多出的 ${tooMany} 张没加`);
      const added = images.slice(0, room.accept).map((file): TrayItem => {
        counter.current += 1;
        const id = crypto.randomUUID();
        files.current.set(id, file);
        return {
          id,
          name: file.name || `图片 ${counter.current}`,
          kind: "image",
          size: file.size,
          previewUrl: URL.createObjectURL(file),
          status: "uploading",
          progress: 0,
        };
      });
      if (added.length === 0) return;
      commit([...latest.current, ...added]);
      for (const item of added) void upload(item.id, files.current.get(item.id)!);
    },
    [commit, upload],
  );

  /** 加文档（＋ 菜单的「上传故事或剧本」）：只收 .txt / .md，单个不超过 20 万字节，一条消息最多 3 个；被拒绝的都提示原因 */
  const addDocuments = useCallback(
    (picked: ArrayLike<File> | null | undefined) => {
      const { accepted, wrongType, tooLarge, empty, clipped } = pickDocumentFiles(
        picked,
        documentCount(latest.current),
      );
      if (wrongType > 0) toast.warning("只支持 .txt 和 .md");
      if (tooLarge > 0) toast.warning("文件太大了，请控制在 20 万字节以内");
      if (empty > 0) toast.warning("这个文件是空的");
      if (clipped > 0)
        toast.warning(`一条消息最多 ${MAX_ATTACH_DOCUMENTS} 个文档，多出的 ${clipped} 个没加`);
      const added = accepted.map((file): TrayItem => {
        const id = crypto.randomUUID();
        files.current.set(id, file);
        return {
          id,
          name: file.name,
          kind: "document",
          size: file.size,
          previewUrl: "",
          status: "uploading",
          progress: 0,
        };
      });
      if (added.length === 0) return;
      commit([...latest.current, ...added]);
      for (const item of added) void upload(item.id, files.current.get(item.id)!);
    },
    [commit, upload],
  );

  const retry = useCallback(
    (id: string) => {
      const file = files.current.get(id);
      if (file) void upload(id, file);
    },
    [upload],
  );

  const remove = useCallback(
    (id: string) => {
      const item = latest.current.find((i) => i.id === id);
      if (!item) return;
      release(item);
      commit(latest.current.filter((i) => i.id !== id));
    },
    [commit, release],
  );

  /** 发送成功后清空托盘 */
  const clear = useCallback(() => {
    for (const item of latest.current) release(item);
    commit([]);
  }, [commit, release]);

  // 面板关掉时取消还在传的、回收预览地址
  useEffect(
    () => () => {
      for (const item of latest.current) release(item);
    },
    [release],
  );

  return { items, add, addDocuments, retry, remove, clear };
}

/** 托盘的操作集：面板持有，输入框用来显示和加图 */
export type AgentAttachments = ReturnType<typeof useAgentAttachments>;
