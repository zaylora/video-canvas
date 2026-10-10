import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent as ReactMouseEvent,
  type Ref,
} from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { Box, Camera, Link2, Music, Shapes } from "lucide-react";
import { useNodesData } from "@xyflow/react";
import { MediaPreview, VideoPoster } from "./media-preview";
import { Node as TiptapNode } from "@tiptap/core";
import Document from "@tiptap/extension-document";
import HardBreak from "@tiptap/extension-hard-break";
import Mention from "@tiptap/extension-mention";
import Paragraph from "@tiptap/extension-paragraph";
import Text from "@tiptap/extension-text";
import { UndoRedo } from "@tiptap/extensions";
import {
  EditorContent,
  NodeViewWrapper,
  ReactNodeViewRenderer,
  useEditor,
  type NodeViewProps,
  type UseEditorOptions,
} from "@tiptap/react";
import type { SuggestionProps } from "@tiptap/suggestion";

import { NODE_META } from "@/constants/canvas";
import { findPreset, type PresetKind } from "@/constants/presets";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { CanvasNode, NodeKind, NodeMediaType } from "@/types";
import { planPreset, type PresetPick, type PresetPlan } from "@/utils/canvas/preset-rules";
import {
  MENTION_NODE,
  PRESET_NODE,
  docToPrompt,
  parsePrompt,
  promptPresets,
  promptToDoc,
} from "@/utils/canvas/prompt-tokens";

/** 能被引用的一份素材：引用条的一格、@ 菜单的一项、chip 的缩略图都按它画 */
export type RefSource = {
  /** 源节点 id */
  id: string;
  /** 源节点种类 */
  kind: NodeKind;
  /** 源节点标题 */
  label: string;
  /** 素材地址；文本节点和还没产出的节点没有 */
  src?: string | null;
  /** src 是图、视频还是音频 */
  mediaType?: NodeMediaType;
  /** 文本节点的正文 */
  text?: string | null;
};

/** 提示词里 @ 素材要的数据，由节点业务层提供 */
export type PromptMentionSource = {
  /** 打开菜单时现取：已经连上的、画布里还没连但接得上的（接不上的不给） */
  list: () => { linked: RefSource[]; canvas: RefSource[] };
  /** 选中了一个素材：还没连线就补一根，和正在打的字算同一步撤销 */
  link: (source: RefSource) => void;
  /** 眼下接进来的上游 id；chip 指向的节点不在里面就算失效 */
  linkedIds: ReadonlySet<string>;
};

/** 提示词里被点中的预设 chip：选择器据此在它旁边打开，用来替换 */
export type PresetChipTarget = {
  kind: PresetKind;
  /** 它是提示词里的第几个预设（按出现顺序） */
  index: number;
  /** chip 的 DOM，选择器锚在它身上 */
  anchor: HTMLElement;
};

/** 节点预设（设计稿 6.13）在面板里的状态和操作，选择器和提示词里的 chip 共用 */
export type PromptPresets = {
  /** 提示词里已有的预设，按出现顺序 */
  selected: readonly PresetPick[];
  /** 当前模型的提示词字数上限，选择器据此提示放不下的预设；没有就不提示 */
  maxLength?: number;
  /** 选中一个预设；target 是从 chip 点进来时那个 chip 的序号。返回执行的方案，被拦下时带原因 */
  apply: (kind: PresetKind, id: string, target?: number) => PresetPlan | undefined;
  /** 把焦点还给提示词 */
  focusEditor: () => void;
  /** 被点中的 chip，没有就是 null */
  chip: PresetChipTarget | null;
  setChip: (chip: PresetChipTarget | null) => void;
};

/** 引用条、chip、画布节点三处联动描边，以及从引用条往提示词里插 chip */
type PromptRefsValue = {
  /** 眼下接进来的上游 id */
  linkedIds: ReadonlySet<string>;
  /** 正被悬停的素材 id，没有就是 null */
  highlight: string | null;
  setHighlight: (id: string | null) => void;
  /** 在提示词光标处插入这个素材的 chip */
  insertRef: (source: RefSource) => void;
  /** 节点预设的状态和操作 */
  presets: PromptPresets;
};

const PromptRefsContext = createContext<PromptRefsValue | null>(null);

export const PromptRefsProvider = PromptRefsContext.Provider;

/** 引用条、chip 读联动状态；不在提示词面板里时返回 null */
export const usePromptRefs = () => useContext(PromptRefsContext);

/**
 * 悬停中的素材 id：除了引用条和 chip 自己描边，画布上的源节点也虚线圈出来。
 * 圈节点直接给 DOM 挂类名，不经过节点的 React 状态，免得整张画布重渲染。
 */
export function useRefHighlight() {
  const [highlight, setHighlight] = useState<string | null>(null);
  useEffect(() => {
    if (!highlight) return;
    const el = document.querySelector(`.react-flow__node[data-id="${CSS.escape(highlight)}"]`);
    el?.classList.add("ref-hl");
    return () => el?.classList.remove("ref-hl");
  }, [highlight]);
  return [highlight, setHighlight] as const;
}

/**
 * 素材缩略图：图片、视频取画面，音频是音符，文本在格子里摆前几十个字（compact 时只给图标），
 * 还没产出的给种类图标。外框大小和圆角由调用方定。
 */
export function RefThumb({ source, compact }: { source: RefSource; compact?: boolean }) {
  const KindIcon = NODE_META.get(source.kind)?.icon;
  if (source.kind === "script") {
    const text = source.text?.trim();
    if (text && !compact)
      return (
        <span className="text-muted-foreground line-clamp-2 size-full px-1.5 pt-5 text-left text-[10px] leading-[1.35]">
          {text.slice(0, 40)}
        </span>
      );
    return (
      <span className="text-muted-foreground grid size-full place-items-center">
        {KindIcon && <KindIcon className="size-[55%] max-w-4" />}
      </span>
    );
  }
  if (!source.src)
    return (
      <span className="text-muted-foreground grid size-full place-items-center">
        {KindIcon && <KindIcon className="size-[45%] max-w-5" />}
      </span>
    );
  const media = source.mediaType ?? source.kind;
  if (media === "image")
    return <MediaPreview src={source.src} mode="thumb" fit="cover" draggable={false} />;
  if (media === "video") return <VideoPoster src={source.src} iconClassName="size-4" />;
  return (
    <span className="text-status-success grid size-full place-items-center">
      <Music className="size-[50%] max-w-5" />
    </span>
  );
}

/**
 * 提示词里的素材 chip：缩略图 + 节点标题（取画布上的最新标题，节点没了才用存下的名字）。
 * 源节点被删、或者连线已经断开时显示删除线，提交时退回成素材名。
 */
function MentionChip({ node }: NodeViewProps) {
  const id = String(node.attrs.id ?? "");
  const saved = String(node.attrs.label ?? "");
  const live = useNodesData<CanvasNode>(id)?.data;
  const refs = usePromptRefs();
  const gone = !live || !(refs?.linkedIds.has(id) ?? true);
  const lit = refs?.highlight === id;
  const label = live?.label ?? saved;

  return (
    <NodeViewWrapper
      as="span"
      data-ref={id}
      title={gone ? "引用已断开：重新连线，或删掉这个引用" : `引用「${label}」`}
      onMouseEnter={() => refs?.setHighlight(id)}
      onMouseLeave={() => refs?.setHighlight(null)}
      className={cn(
        "mx-0.5 inline-flex h-6.5 max-w-42 cursor-default items-center gap-1.5 rounded-md py-0 pr-2 pl-[3px] align-middle text-[13px] font-semibold",
        "ring-chrome-border ring-1 ring-inset transition-[box-shadow,background-color] duration-120",
        "animate-in fade-in-0 zoom-in-90 duration-120 motion-reduce:zoom-in-100",
        gone ? "text-muted-foreground" : "bg-foreground/[0.07]",
        lit && "ring-node-ring bg-foreground/12 ring-[1.5px]",
      )}
    >
      <span
        className={cn(
          "bg-foreground/10 size-5 shrink-0 overflow-hidden rounded-[5px]",
          gone && "opacity-35",
        )}
      >
        <RefThumb
          compact
          source={{
            id,
            kind: live?.kind ?? "image",
            label,
            src: live?.src,
            mediaType: live?.mediaType,
            text: live?.text,
          }}
        />
      </span>
      <span className={cn("truncate", gone && "line-through")}>{label}</span>
    </NodeViewWrapper>
  );
}

/** 预设 chip 的图标：风格、运镜、模板各一个 */
const PRESET_ICON = { style: Box, motion: Camera, tpl: Shapes } as const;

/**
 * 提示词里的预设 chip（设计稿 6.13）：紫色图标 + 名称，和素材 chip 同尺寸。
 * 点它在旁边弹出同类选择器用来替换；预设已下架时删除线，发送时按普通文字。
 */
function PresetChip({ node, editor, getPos, extension }: NodeViewProps) {
  const kind = node.attrs.kind as PresetKind;
  const id = String(node.attrs.id ?? "");
  const preset = findPreset(kind, id);
  const refs = usePromptRefs();
  const Icon = PRESET_ICON[kind] ?? Box;
  const label = preset?.name ?? String(node.attrs.label ?? "");
  const clickable = !!extension.options.clickable && !!refs;

  const open = (event: ReactMouseEvent<HTMLElement>) => {
    if (!clickable) return;
    const pos = getPos();
    if (pos === undefined) return;
    let index = 0;
    editor.state.doc.nodesBetween(0, pos, (child) => {
      if (child.type.name === PRESET_NODE) index += 1;
    });
    refs?.presets.setChip({ kind, index, anchor: event.currentTarget });
  };

  return (
    <NodeViewWrapper
      as="span"
      data-preset={`${kind}/${id}`}
      title={
        preset
          ? `「${label}」${preset.prompt.slice(0, 80)}${preset.prompt.length > 80 ? "…" : ""}${clickable ? "（点击替换）" : ""}`
          : "这个预设已不存在，发送时按普通文字处理"
      }
      onClick={open}
      className={cn(
        "mx-0.5 inline-flex h-6.5 max-w-42 items-center gap-1.5 rounded-md py-0 pr-2 pl-[3px] align-middle text-[13px] font-semibold",
        "ring-chrome-border ring-1 transition-[background-color] duration-120 ring-inset",
        "animate-in fade-in-0 zoom-in-90 duration-120 motion-reduce:zoom-in-100",
        preset ? "bg-foreground/[0.07]" : "text-muted-foreground",
        clickable ? "hover:bg-foreground/12 cursor-pointer" : "cursor-default",
      )}
    >
      <span
        className={cn(
          "bg-preset grid size-5 shrink-0 place-items-center rounded-[5px] text-white",
          !preset && "opacity-35",
        )}
      >
        <Icon className="size-3" />
      </span>
      <span className={cn("truncate", !preset && "line-through")}>{label}</span>
    </NodeViewWrapper>
  );
}

/** @ 菜单里的一项 */
type MenuEntry = {
  source: RefSource;
  /** 已经连上了；否则选中时会自动连线 */
  linked: boolean;
};

/** 打开中的 @ 菜单 */
type MenuState = {
  /** @ 后面已经输入的字 */
  query: string;
  /** 光标位置，菜单贴着它摆 */
  rect: DOMRect | null;
  /** 把选中的素材写进提示词（替换掉 @ 和查询字） */
  command: (attrs: { id: string; label: string }) => void;
};

/** 菜单的估算高度：用来决定贴在光标下方还是翻到上方，不必先渲染再量 */
const estimateHeight = (rows: number, groups: number) =>
  Math.min(360, window.innerHeight * 0.6, 12 + Math.max(1, rows) * 40 + groups * 28);

/**
 * @ 菜单（设计稿 6.7）：贴着光标出现，下方放不下就翻到上方；
 * 分「已引用」和「画布中 · 选中后自动连线」两组，接不上的素材不出现。
 * 挂在 body 上，盖得住放大编辑的对话框。
 */
function MentionMenu({
  menu,
  entries,
  active,
  onHover,
  onPick,
}: {
  menu: MenuState;
  entries: MenuEntry[];
  active: number;
  onHover: (index: number) => void;
  onPick: (entry: MenuEntry) => void;
}) {
  const reduce = useReducedMotion();
  const listRef = useRef<HTMLDivElement>(null);
  const linked = entries.filter((entry) => entry.linked);
  const canvas = entries.filter((entry) => !entry.linked);

  useEffect(() => {
    listRef.current
      ?.querySelector(`[data-index="${active}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [active]);

  const rect = menu.rect;
  if (!rect) return null;
  const width = Math.min(300, window.innerWidth - 16);
  const height = estimateHeight(
    entries.length,
    (linked.length > 0 ? 1 : 0) + (canvas.length > 0 ? 1 : 0),
  );
  const up = rect.bottom + 6 + height > window.innerHeight - 8 && rect.top - height - 6 > 8;
  const left = Math.min(Math.max(8, rect.left - 14), window.innerWidth - width - 8);
  const dy = reduce ? 0 : up ? 4 : -4;

  const row = (entry: MenuEntry) => {
    const index = entries.indexOf(entry);
    return (
      <button
        key={entry.source.id}
        type="button"
        role="option"
        aria-selected={index === active}
        data-index={index}
        onMouseMove={() => index !== active && onHover(index)}
        onClick={() => onPick(entry)}
        className={cn(
          "flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left text-[13px] outline-none",
          index === active && "bg-chrome-hover",
        )}
      >
        <span className="bg-foreground/10 size-7 shrink-0 overflow-hidden rounded-md">
          <RefThumb compact source={entry.source} />
        </span>
        <span className="min-w-0 flex-1 truncate">{entry.source.label}</span>
        {!entry.linked && (
          <span className="text-muted-foreground flex shrink-0 items-center gap-0.5 text-[11px]">
            <Link2 className="size-3" />
            连线
          </span>
        )}
      </button>
    );
  };

  return createPortal(
    <motion.div
      ref={listRef}
      role="listbox"
      aria-label="引用画布里的素材"
      // 点菜单不能抢走编辑器的焦点，否则光标和 @ 的位置就丢了
      onMouseDown={(event) => event.preventDefault()}
      initial={{ opacity: 0, scale: reduce ? 1 : 0.96, y: dy }}
      animate={{
        opacity: 1,
        scale: 1,
        y: 0,
        transition: { duration: DURATION.base, ease: EASE_OUT },
      }}
      exit={{
        opacity: 0,
        scale: reduce ? 1 : 0.96,
        y: dy,
        transition: { duration: DURATION.base * 0.7, ease: EASE_OUT },
      }}
      style={{
        left,
        width,
        maxHeight: height,
        ...(up ? { bottom: window.innerHeight - rect.top + 6 } : { top: rect.bottom + 6 }),
        transformOrigin: `${rect.left - left}px ${up ? "100%" : "0"}`,
      }}
      className="bg-popover text-popover-foreground ring-chrome-border nowheel fixed z-[70] overflow-y-auto rounded-xl p-1.5 shadow-2xl ring-1"
    >
      {linked.length > 0 && (
        <>
          <p className="text-muted-foreground px-2 pt-1.5 pb-1 text-[11px]">已引用</p>
          {linked.map(row)}
        </>
      )}
      {canvas.length > 0 && (
        <>
          <p className="text-muted-foreground px-2 pt-1.5 pb-1 text-[11px]">
            画布中 · 选中后自动连线
          </p>
          {canvas.map(row)}
        </>
      )}
      {entries.length === 0 && (
        <p className="text-muted-foreground px-2.5 py-2.5 text-xs">
          {menu.query ? `没有匹配「${menu.query}」的素材` : "画布里还没有能引用的素材"}
        </p>
      )}
    </motion.div>,
    document.body,
  );
}

/**
 * 编辑器的扩展：段落 + 硬换行 + 自带撤销 + @ 素材。只在编辑器创建时建一次，
 * 所以这里只碰稳定的 setter；菜单按键要读最新的筛选结果，放在 editorProps.handleKeyDown 里做。
 */
function promptExtensions(
  setMenu: (menu: MenuState | null) => void,
  setActive: (index: number) => void,
  /** 预设 chip 能不能点开选择器；放大编辑里的不能 */
  presetClickable: boolean,
) {
  const showMenu = (props: SuggestionProps) => {
    setMenu({ query: props.query, rect: props.clientRect?.() ?? null, command: props.command });
    setActive(0);
  };
  /** 预设 chip：行内原子节点，kind / id / label 三个属性，存回提示词是 `#[名称](种类/id)` */
  const Preset = TiptapNode.create<{ clickable: boolean }>({
    name: PRESET_NODE,
    group: "inline",
    inline: true,
    atom: true,
    addOptions: () => ({ clickable: presetClickable }),
    addAttributes: () => ({
      kind: { default: "style" },
      id: { default: "" },
      label: { default: "" },
    }),
    renderText: ({ node }) => String(node.attrs.label ?? ""),
    renderHTML: ({ node }) => ["span", { "data-preset": `${node.attrs.kind}/${node.attrs.id}` }],
    addNodeView: () => ReactNodeViewRenderer(PresetChip, { as: "span" }),
  });
  return [
    Document,
    Paragraph,
    Text,
    HardBreak,
    UndoRedo,
    Preset,
    Mention.extend({
      addNodeView: () => ReactNodeViewRenderer(MentionChip, { as: "span" }),
    }).configure({
      renderText: ({ node }) => String(node.attrs.label ?? ""),
      // 退格删掉整个 chip，不退回成 @ 再弹菜单
      deleteTriggerWithBackspace: true,
      suggestion: {
        char: "@",
        // 中文前面没有空格，@ 紧跟在字后面也要能触发
        allowedPrefixes: null,
        // 面板小框和放大编辑的大框绑同一份提示词：大框里打 @ 会同步进小框、光标正好落在 @ 后面，
        // 只认手里有焦点的那一个，否则两个框各弹一个菜单
        allow: ({ editor }) => editor.view.hasFocus(),
        items: () => [],
        render: () => ({
          onStart: showMenu,
          onUpdate: showMenu,
          onExit: () => setMenu(null),
        }),
      },
    }),
  ];
}

/** 提示词编辑器对外的操作 */
export type PromptEditorHandle = {
  /** 在光标处插入一个素材 chip，后面带一个空格 */
  insertRef: (source: RefSource) => void;
  /**
   * 选中一个节点预设（设计稿 6.13）：按数量规则取消、替换或插入。模板插在最前面，
   * 风格和运镜插在最后一次的光标处（没点过编辑器就在末尾），后面带一个空格。
   */
  applyPreset: (kind: PresetKind, id: string, target?: number) => PresetPlan | undefined;
  /** 把焦点还给编辑器 */
  focus: () => void;
};

/**
 * 提示词编辑器：纯文本 + 素材 chip（设计稿 6.7）。value 是带 `@[名字](id)` token 的纯文本，
 * 输入 @ 弹出引用菜单。小框 Enter 发送、Shift+Enter 换行；大框 Enter 换行、⌘Enter 发送；组字中的 Enter 不算。
 */
export function PromptEditor({
  ref,
  value,
  onValueChange,
  placeholder,
  disabled,
  onSubmit,
  large,
  autoFocus,
  mention,
}: {
  ref?: Ref<PromptEditorHandle>;
  value: string;
  onValueChange: (value: string) => void;
  placeholder: string;
  disabled?: boolean;
  onSubmit: () => void;
  /** 放大编辑对话框里的那一个 */
  large?: boolean;
  autoFocus?: boolean;
  /** 不给就不能 @ */
  mention?: PromptMentionSource;
}) {
  const [menu, setMenu] = useState<MenuState | null>(null);
  const [active, setActive] = useState(0);

  const entries = useMemo<MenuEntry[]>(() => {
    if (!menu || !mention) return [];
    const { linked, canvas } = mention.list();
    const query = menu.query.toLowerCase();
    const match = (source: RefSource) => !query || source.label.toLowerCase().includes(query);
    return [
      ...linked.filter(match).map((source) => ({ source, linked: true })),
      ...canvas.filter(match).map((source) => ({ source, linked: false })),
    ];
  }, [menu, mention]);
  const current = Math.min(active, Math.max(0, entries.length - 1));

  /** 选中菜单里的一项：还没连线的先连上，再把 @ 和查询字换成 chip */
  const pick = useCallback(
    (entry: MenuEntry | undefined) => {
      if (!entry || !menu) return;
      if (!entry.linked) mention?.link(entry.source);
      menu.command({ id: entry.source.id, label: entry.source.label });
    },
    [menu, mention],
  );

  // 扩展和初始内容只在创建时用一次；editorProps、onUpdate 每次渲染都会被 useEditor 换成最新的
  const [extensions] = useState(() => promptExtensions(setMenu, setActive, !large));
  /** 用户点过编辑器：没点过的话，插风格和运镜就放在末尾，而不是文档开头 */
  const touched = useRef(false);
  const [initialContent] = useState(() => promptToDoc(value));
  const editorProps = useMemo<UseEditorOptions["editorProps"]>(
    () => ({
      attributes: {
        "aria-label": "提示词",
        "aria-multiline": "true",
        role: "textbox",
        // 画布上按 Enter 时靠它找到并聚焦面板里的提示词
        ...(large ? {} : { "data-node-prompt": "" }),
        // nowheel 把滚轮留给编辑器，别让画布跟着平移
        class: cn(
          "nowheel w-full overflow-y-auto px-1 text-[15px] leading-[1.75] break-words whitespace-pre-wrap outline-none",
          large ? "max-h-[60vh] min-h-72" : "max-h-50 min-h-18",
        ),
      },
      // 直接挂在 view 上的按键先于 suggestion 插件处理，菜单的上下、选中就在这里接
      handleKeyDown: (_view, event) => {
        if (event.isComposing) return false;
        if (menu) {
          // 只关菜单（交给 suggestion 插件），别让放大编辑的对话框也跟着收起
          if (event.key === "Escape") {
            event.stopPropagation();
            return false;
          }
          if (entries.length === 0) return false;
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            const step = event.key === "ArrowDown" ? 1 : -1;
            setActive((current + step + entries.length) % entries.length);
            return true;
          }
          if (event.key === "Enter" || event.key === "Tab") {
            pick(entries[current]);
            return true;
          }
          return false;
        }
        if (event.key !== "Enter" || event.shiftKey) return false;
        // 大编辑框里写长文，回车只换行，⌘Enter 才发
        if (large && !event.metaKey && !event.ctrlKey) return false;
        event.preventDefault();
        onSubmit();
        return true;
      },
    }),
    [current, entries, large, menu, onSubmit, pick],
  );

  const editor = useEditor({
    extensions,
    content: initialContent,
    editable: !disabled,
    autofocus: autoFocus ? "end" : false,
    shouldRerenderOnTransaction: false,
    editorProps,
    onFocus: () => {
      touched.current = true;
    },
    onUpdate: ({ editor: self }) => onValueChange(docToPrompt(self.getJSON())),
  });

  // 撤销、换模型这类外部改动：内容对不上才重灌，自己打字回传的不动，免得光标乱跳
  useEffect(() => {
    if (!editor || docToPrompt(editor.getJSON()) === value) return;
    editor.commands.setContent(promptToDoc(value), { emitUpdate: false });
  }, [editor, value]);

  useEffect(() => {
    editor?.setEditable(!disabled, false);
  }, [disabled, editor]);

  useImperativeHandle(
    ref,
    () => ({
      insertRef: (source) => {
        editor
          ?.chain()
          .focus()
          .insertContent([
            { type: MENTION_NODE, attrs: { id: source.id, label: source.label } },
            { type: "text", text: " " },
          ])
          .run();
      },
      applyPreset: (kind, id, target) => {
        if (!editor) return undefined;
        const { doc, schema } = editor.state;
        const found: { pick: PresetPick; pos: number; size: number }[] = [];
        doc.descendants((node, pos) => {
          if (node.type.name === PRESET_NODE)
            found.push({
              pick: { kind: node.attrs.kind as PresetKind, id: String(node.attrs.id) },
              pos,
              size: node.nodeSize,
            });
        });
        const plan = planPreset(
          found.map((item) => item.pick),
          kind,
          id,
          target,
        );
        const chip = () =>
          schema.nodes[PRESET_NODE].create({ kind, id, label: findPreset(kind, id)?.name ?? id });
        const tr = editor.state.tr;
        if (plan.type === "remove") {
          const { pos, size } = found[plan.index];
          // 插 chip 时带的那个空格一起拿掉
          const spaced =
            doc.textBetween(pos + size, Math.min(doc.content.size, pos + size + 1)) === " ";
          tr.delete(pos, pos + size + (spaced ? 1 : 0));
        } else if (plan.type === "replace") {
          const { pos, size } = found[plan.index];
          tr.replaceWith(pos, pos + size, chip());
        } else if (plan.type === "insert") {
          const at =
            plan.at === "start"
              ? 1
              : touched.current
                ? editor.state.selection.to
                : doc.content.size - 1;
          tr.insert(at, [chip(), schema.text(" ")]);
        } else return plan;
        editor.view.dispatch(tr);
        return plan;
      },
      focus: () => {
        editor?.commands.focus();
      },
    }),
    [editor],
  );

  // 提示词里只放了模板、还没写字：占位说明跟在模板 chip 后面
  const templateHint = useMemo(() => {
    const tpl = promptPresets(value).find((seg) => seg.kind === "tpl");
    if (!tpl) return null;
    const written = parsePrompt(value).some(
      (seg) => seg.type === "ref" || (seg.type === "text" && seg.text.trim() !== ""),
    );
    const preset = findPreset("tpl", tpl.id);
    return written || !preset ? null : `${preset.usage}。可以补充说明，也可以直接发送`;
  }, [value]);

  return (
    <>
      <div
        className={cn("relative", disabled && "opacity-60", templateHint && "prompt-tpl-hint")}
        style={
          templateHint
            ? ({ "--tpl-hint": JSON.stringify(templateHint) } as CSSProperties)
            : undefined
        }
      >
        <EditorContent editor={editor} />
        {!value && (
          <span className="text-muted-foreground/70 pointer-events-none absolute inset-x-1 top-0 text-[15px] leading-[1.75]">
            {placeholder}
          </span>
        )}
      </div>
      <AnimatePresence>
        {menu && (
          <MentionMenu
            key="mention-menu"
            menu={menu}
            entries={entries}
            active={current}
            onHover={setActive}
            onPick={pick}
          />
        )}
      </AnimatePresence>
    </>
  );
}
