import { motion } from "motion/react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { CircleX, Languages, Loader2, RefreshCw, TriangleAlert } from "lucide-react";

import { isRevisionConflict } from "@/api/drama/errors";
import type { AdaptFormat, Segment } from "@/api/drama/type";
import {
  PromptChipResolverProvider,
  PromptEditor,
  type ChipResolver,
  type PromptEditorHandle,
  type PromptMentionSource,
  type RefSource,
} from "@/components/canvas/prompt-mention";
import { Button } from "@/components/ui/button";
import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { formatLabel } from "@/utils/drama/adaptation";
import { Autosaver } from "@/utils/drama/autosave";
import { splitByEpisode, type RefAsset } from "@/utils/drama/segment-assets";

import { ASSET_DRAG_TYPE } from "./library";
import { adaptKey, useStudio, type DraftKind } from "./use-studio";

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
 * 片段的提示词区：原稿固定是 Seedance 写法。
 * 顶栏模型是 Seedance 时只有原稿；是 H3 / 通用时多一个转写标签，可以把原稿转写成那种格式、对照、手改。
 */
export function PromptPane({ segment }: { segment: Segment }) {
  const { format } = useStudio();
  if (format === "seedance") {
    return <AutosavedPrompt segment={segment} kind="source" />;
  }
  // 换格式整块重挂：默认标签按新格式重新判断
  return <AdaptTabs key={format} segment={segment} format={format} />;
}

type View = "source" | "adapted";

/** 原稿 / 转写两个标签 + 转写按钮 */
function AdaptTabs({ segment, format }: { segment: Segment; format: AdaptFormat }) {
  const { actions, adaptStateOf, adaptErrors } = useStudio();
  const adaptation = segment.adaptations[format];
  const [view, setView] = useState<View>(adaptation ? "adapted" : "source");
  const state = adaptStateOf(segment);
  const busy = state === "busy";
  const error = adaptErrors[adaptKey(segment.id, format)];
  const label = formatLabel(format);

  const run = useCallback(async () => {
    setView("adapted");
    await actions.adapt(segment);
  }, [actions, segment]);

  return (
    <div className="grid gap-2.5">
      <div className="flex flex-wrap items-center gap-2">
        <div
          role="tablist"
          aria-label="提示词格式"
          className="bg-chrome ring-chrome-border relative flex rounded-full p-0.5 shadow-sm ring-1 backdrop-blur-xl backdrop-saturate-150"
        >
          {(
            [
              ["source", "原稿 · Seedance"],
              ["adapted", `转写 · ${label}`],
            ] as const
          ).map(([value, text]) => (
            <button
              key={value}
              type="button"
              role="tab"
              aria-selected={view === value}
              onClick={() => setView(value)}
              className={cn(
                "focus-visible:ring-ring/50 relative flex h-7 items-center gap-1.5 rounded-full px-3 text-xs font-medium outline-none focus-visible:ring-2",
                view === value ? "text-foreground" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {view === value && (
                <motion.span
                  layoutId="studio-prompt-format"
                  transition={SPRING}
                  className="bg-chrome-hover ring-chrome-border absolute inset-0 rounded-full ring-1"
                />
              )}
              <span className="relative">{text}</span>
              {value === "adapted" && <AdaptDot state={state} />}
            </button>
          ))}
        </div>
        <span className="flex-1" />
        <Button
          variant="ghost"
          size="sm"
          className="rounded-full"
          disabled={busy}
          title={busy ? "正在转写" : adaptation ? `按当前原稿重新转写为 ${label}` : undefined}
          onClick={() => void run()}
        >
          {busy ? <Loader2 className="animate-spin" /> : adaptation ? <RefreshCw /> : <Languages />}
          {adaptation ? "重新转写" : `转写为 ${label}`}
        </Button>
      </div>

      {view === "source" ? (
        <AutosavedPrompt segment={segment} kind="source" />
      ) : (
        <div className="grid gap-2">
          {error && (
            <Bar tone="destructive" icon={<CircleX />} action="重试" onAction={() => void run()}>
              {error}
            </Bar>
          )}
          {state === "stale" && !busy && (
            <Bar
              tone="warning"
              icon={<TriangleAlert />}
              action="重新转写"
              onAction={() => void run()}
            >
              原稿已修改
            </Bar>
          )}
          {adaptation ? (
            <AutosavedPrompt
              key={`${segment.id}:${format}`}
              segment={segment}
              kind={format}
              disabled={busy}
            />
          ) : (
            <div className="bg-panel border-border text-muted-foreground grid min-h-36 place-items-center content-center gap-3 rounded-[14px] border px-4 py-8 text-sm">
              {busy ? (
                <Loader2 className="size-6 animate-spin" />
              ) : (
                <Languages className="size-6" />
              )}
              <span>{busy ? `正在转写为 ${label}` : `还没有 ${label} 转写稿`}</span>
              {!busy && (
                <Button
                  size="sm"
                  variant="outline"
                  className="rounded-full"
                  onClick={() => void run()}
                >
                  转写为 {label}
                </Button>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

/** 转写标签上的状态：转圈 = 转写中，绿点 = 最新，黄点 + 已过期 */
function AdaptDot({ state }: { state: ReturnType<ReturnType<typeof useStudio>["adaptStateOf"]> }) {
  if (state === "busy") return <Loader2 className="relative size-3 animate-spin" />;
  if (state === "fresh") {
    return <span aria-label="最新" className="bg-status-success relative size-1.5 rounded-full" />;
  }
  if (state === "stale") {
    return (
      <span className="text-status-warning relative flex items-center gap-1">
        <span className="bg-status-warning size-1.5 rounded-full" />
        已过期
      </span>
    );
  }
  return null;
}

/** 提示条：错误 / 过期，右侧一个操作 */
function Bar({
  tone,
  icon,
  action,
  onAction,
  children,
}: {
  tone: "destructive" | "warning";
  icon: React.ReactNode;
  action: string;
  onAction: () => void;
  children: React.ReactNode;
}) {
  return (
    <div
      role={tone === "destructive" ? "alert" : "status"}
      className={cn(
        "flex items-center gap-2 rounded-lg border px-3 py-1.5 text-sm [&>svg]:size-4 [&>svg]:shrink-0",
        tone === "destructive"
          ? "bg-destructive/8 border-destructive/25 [&>svg]:text-destructive"
          : "bg-status-warning/10 border-status-warning/25 [&>svg]:text-status-warning",
      )}
    >
      {icon}
      <span className="min-w-0 flex-1">{children}</span>
      <Button variant="ghost" size="xs" className="shrink-0" onClick={onAction}>
        {action}
      </Button>
    </div>
  );
}

/**
 * 一份可编辑的提示词（原稿或某格式的转写稿）：复用无限画布的 Tiptap @ 编辑器（chip、@ 菜单、撤销）。
 * - @ 候选是本剧资产节点，本集优先；序列化为 `@[名](节点id)`；从资产库拖入时在落点插入同一种 chip；
 * - 停手后自动保存（patch 里按片段串行、用 store 最新 revision），⌘↵ 立即生成；
 * - 服务端内容变了（别处改了 / 重新转写 / 冲突后加载最新）：本地没有未保存改动就跟着换。
 */
function AutosavedPrompt({
  segment,
  kind,
  disabled,
}: {
  segment: Segment;
  kind: DraftKind;
  disabled?: boolean;
}) {
  const studio = useStudio();
  const { assets, assetsStatus, no, actions, reportSave, registerEditor, setDraft } = studio;
  const format = kind === "source" ? null : (kind as AdaptFormat);
  const server = format ? (segment.adaptations[format]?.prompt ?? "") : segment.prompt;
  const editorRef = useRef<PromptEditorHandle>(null);
  const [value, setValue] = useState(server);
  const [over, setOver] = useState(false);
  const lastSaved = useRef(server);
  const latest = useRef({ patch: actions.patch, reportSave });
  latest.current = { patch: actions.patch, reportSave };
  const saveKey = format ? `adapt:${format}` : "prompt";
  const [saver] = useState(
    () =>
      new Autosaver({
        revision: segment.revision,
        isConflict: isRevisionConflict,
        save: async (text) => {
          const next = await latest.current.patch(
            segment.id,
            format ? { adaptation: { format, prompt: text } } : { prompt: text },
          );
          lastSaved.current = text;
          return next.revision;
        },
        onStatus: (status) => latest.current.reportSave(saveKey, status),
      }),
  );

  // 挂载：以服务端内容为基线
  useEffect(() => {
    saver.reset(server, segment.revision);
    setDraft(segment.id, server, kind);
    // 只在挂载时执行；之后的外部变化走下面的 effect
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 服务端内容变了
  useEffect(() => {
    if (server === lastSaved.current) return;
    const status = saver.getStatus();
    if (status === "saved" || status === "conflict") {
      lastSaved.current = server;
      saver.reset(server, segment.revision);
      setValue(server);
      setDraft(segment.id, server, kind);
    }
  }, [kind, saver, segment.id, segment.revision, server, setDraft]);

  // 把「立即存盘」登记给工作区，供生成 / 转写前存盘用
  useEffect(() => registerEditor({ flush: () => saver.flush() }), [registerEditor, saver]);

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
      setDraft(segment.id, text, kind);
      saver.update(text);
    },
    [kind, saver, segment.id, setDraft],
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
        "bg-panel border-border rounded-[14px] border px-3.5 py-3 transition-[border-color,box-shadow,opacity]",
        "focus-within:border-ring",
        over && "ring-status-running/50 ring-2",
        disabled && "opacity-60",
      )}
    >
      <PromptChipResolverProvider value={resolver}>
        <PromptEditor
          ref={editorRef}
          large
          value={value}
          onValueChange={onChange}
          disabled={disabled}
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
