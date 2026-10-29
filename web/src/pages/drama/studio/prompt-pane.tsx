import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { isRevisionConflict } from "@/api/drama/errors";
import type { Segment } from "@/api/drama/type";
import {
  PromptChipResolverProvider,
  PromptEditor,
  type ChipResolver,
  type PromptEditorHandle,
  type PromptMentionSource,
  type RefSource,
} from "@/components/canvas/prompt-mention";
import { cn } from "@/lib/utils";
import { Autosaver } from "@/utils/drama/autosave";
import { splitByEpisode, type RefAsset } from "@/utils/drama/segment-assets";

import { ASSET_DRAG_TYPE } from "./library";
import { useStudio } from "./use-studio";

/** 拖进来的资产：dataTransfer 里 JSON 写着节点 id 和名字；不是资产返回 null */
export function readDroppedAsset(data: DataTransfer): { id: string; label: string } | null {
  const raw = data.getData(ASSET_DRAG_TYPE);
  if (!raw) return null;
  try {
    const parsed = JSON.parse(raw) as { id?: unknown; label?: unknown };
    return typeof parsed.id === "string" && typeof parsed.label === "string"
      ? { id: parsed.id, label: parsed.label }
      : null;
  } catch {
    return null;
  }
}

/**
 * 片段的提示词编辑器：复用无限画布的 Tiptap @ 编辑器（chip、@ 菜单、撤销）。
 * - @ 候选是本剧资产节点，本集优先；序列化为 `@[名](节点id)`；从资产库拖入时在落点插入同一种 chip；
 * - 停手后自动保存（带 revision），⌘↵ 立即生成；换片段整块重新挂载（key = 片段 id）；
 * - 服务端内容变了（别处改了 / 冲突后加载最新）：本地没有未保存改动就跟着换。
 */
export function PromptPane({ segment }: { segment: Segment }) {
  const studio = useStudio();
  const { assets, assetsStatus, no, actions, reportSave, registerEditor, setDraft } = studio;
  const editorRef = useRef<PromptEditorHandle>(null);
  const [value, setValue] = useState(segment.prompt);
  const [over, setOver] = useState(false);
  const lastSaved = useRef(segment.prompt);
  const latest = useRef({ patch: actions.patch, reportSave });
  latest.current = { patch: actions.patch, reportSave };
  const [saver] = useState(
    () =>
      new Autosaver({
        revision: segment.revision,
        isConflict: isRevisionConflict,
        save: async (text) => {
          const next = await latest.current.patch(segment.id, { prompt: text });
          lastSaved.current = text;
          return next.revision;
        },
        onStatus: (status) => latest.current.reportSave("prompt", status),
      }),
  );

  // 挂载：以服务端内容为基线，并让「立即生成」前能先存盘
  useEffect(() => {
    saver.reset(segment.prompt, segment.revision);
    setDraft(segment.id, segment.prompt);
    // 只在挂载时执行；之后的外部变化走下面的 effect
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 服务端内容变了
  useEffect(() => {
    if (segment.prompt === lastSaved.current) return;
    const status = saver.getStatus();
    if (status === "saved" || status === "conflict") {
      lastSaved.current = segment.prompt;
      saver.reset(segment.prompt, segment.revision);
      setValue(segment.prompt);
      setDraft(segment.id, segment.prompt);
    }
  }, [saver, segment.id, segment.prompt, segment.revision, setDraft]);

  // 把「立即存盘」登记给工作区，供生成前存盘用
  const assetsById = useMemo(() => new Map(assets.map((asset) => [asset.id, asset])), [assets]);
  const toSource = useCallback(
    (asset: RefAsset): RefSource => ({
      id: asset.id,
      kind: asset.kind,
      label: asset.label,
      src: asset.src,
      mediaType: "image",
    }),
    [],
  );
  useEffect(() => {
    registerEditor({
      flush: () => saver.flush(),
    });
    return () => registerEditor(null);
  }, [registerEditor, saver]);

  // 离开 / 页面隐藏时立即保存，不丢改动
  useEffect(() => {
    const flush = () => void saver.flush();
    const onHide = () => {
      if (document.visibilityState === "hidden") flush();
    };
    document.addEventListener("visibilitychange", onHide);
    window.addEventListener("beforeunload", flush);
    return () => {
      document.removeEventListener("visibilitychange", onHide);
      window.removeEventListener("beforeunload", flush);
      void saver.flush().finally(() => saver.dispose());
    };
  }, [saver]);

  const mention = useMemo<PromptMentionSource>(
    () => ({
      list: () => {
        // 没有形象图的角色也列出来：选了只是暂时只用文字，补上图之后自然带入
        const { here, other } = splitByEpisode(assets, no);
        return { linked: here.map(toSource), canvas: other.map(toSource) };
      },
      link: () => undefined,
      linkedIds: new Set<string>(),
      plain: { linkedTitle: "本集资产", canvasTitle: "其他集", empty: "还没有可引用的资产" },
    }),
    [assets, no, toSource],
  );

  // chip 的素材信息按 id 查本剧资产；资产还没读到时先当它存在，别闪一下「已失效」
  const resolver = useCallback<ChipResolver>(
    (id) => {
      const asset = assetsById.get(id);
      if (!asset) return assetsStatus === "ready" ? undefined : { kind: "image", label: "" };
      return {
        kind: asset.kind,
        label: asset.label,
        src: asset.src,
        mediaType: "image",
      };
    },
    [assetsById, assetsStatus],
  );

  const onChange = useCallback(
    (text: string) => {
      setValue(text);
      setDraft(segment.id, text);
      saver.update(text);
    },
    [saver, segment.id, setDraft],
  );

  return (
    <div
      onDragOver={(event) => {
        if (event.dataTransfer.types.includes(ASSET_DRAG_TYPE)) setOver(true);
      }}
      onDragLeave={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setOver(false);
      }}
      onDrop={() => setOver(false)}
      className={cn(
        "bg-panel border-border rounded-[14px] border px-3.5 py-3 transition-[border-color,box-shadow]",
        "focus-within:border-ring",
        over && "ring-status-running/50 ring-2",
      )}
    >
      <PromptChipResolverProvider value={resolver}>
        <PromptEditor
          ref={editorRef}
          large
          value={value}
          onValueChange={onChange}
          placeholder="描述这个片段的画面，输入 @ 引用角色、场景、道具，或从左侧拖入"
          onSubmit={() => void actions.generate(segment)}
          mention={mention}
          acceptDrop={readDroppedAsset}
          className="max-h-[50vh] min-h-36 text-[13.5px] leading-[2.05]"
        />
      </PromptChipResolverProvider>
    </div>
  );
}
