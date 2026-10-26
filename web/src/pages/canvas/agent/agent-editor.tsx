import { useEffect, useImperativeHandle, useMemo, useRef, useState, type Ref } from "react";
import {
  Box,
  Clapperboard,
  FileText,
  Group,
  Image as ImageIcon,
  Music,
  Paperclip,
  Lightbulb,
  UserRound,
} from "lucide-react";
import Document from "@tiptap/extension-document";
import HardBreak from "@tiptap/extension-hard-break";
import Mention from "@tiptap/extension-mention";
import Paragraph from "@tiptap/extension-paragraph";
import Text from "@tiptap/extension-text";
import { Extension } from "@tiptap/core";
import { UndoRedo } from "@tiptap/extensions";
import { Plugin, PluginKey, type EditorState } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import {
  EditorContent,
  NodeViewWrapper,
  ReactNodeViewRenderer,
  useEditor,
  type NodeViewProps,
} from "@tiptap/react";
import Suggestion, { type SuggestionProps } from "@tiptap/suggestion";

import { toast } from "sonner";

import type { NodeKind } from "@/constants/canvas/node-library";
import { cn } from "@/lib/utils";
import type { Chip, ChipType } from "@/utils/agent/chips";
import { chipNode, docToMessage, messageToDoc, type DocNode } from "@/utils/agent/editor-doc";
import type { AgentSkillDto } from "@/api/agent/type.d";
import { ghostContent, nextDismissed, pendingRefs } from "@/utils/agent/selection-ghost";
import type { RefNode, SelectionRef } from "@/utils/agent/selection-refs";
import { MAX_ATTACH_IMAGES, pickImageFiles } from "@/utils/agent/image-files";
import { filterSkills, skillMenuNotice } from "@/utils/agent/skills";

import { SuggestionMenu } from "./suggestion-menu";
import { useSkillCatalog } from "./use-skill-catalog";

/** 能用 @ 引用的节点 */
export type NodeOption = {
  /** 节点 id */
  id: string;
  /** 标题 */
  label: string;
  /** 节点种类（导演台 Agent 读不懂，不在这里） */
  kind: Exclude<NodeKind, "director">;
};

/** 编辑器对外的操作 */
export type AgentEditorHandle = {
  /** 在光标处插入一个 chip，后面带一个空格 */
  insertChip: (chip: Chip) => void;
  /** 在光标处插入文字 */
  insertText: (text: string) => void;
  /** 整个换成这段消息文本（里面的 @[名字](类型:id) 还原成 chip），光标放到末尾 */
  setText: (text: string) => void;
  /** 清空 */
  clear: () => void;
  /** 把焦点放回编辑器 */
  focus: () => void;
};

const CHIP_TONE: Record<ChipType, string> = {
  node: "bg-foreground/7",
  model: "bg-status-running/20",
  skill: "bg-preset/20",
  asset: "bg-foreground/7",
};

const KIND_ICON = {
  script: FileText,
  image: ImageIcon,
  video: Clapperboard,
  audio: Music,
  character: UserRound,
} as const;

/** 行内 chip：不可编辑的原子节点，退格整个删除 */
function ChipView({ node }: NodeViewProps) {
  const type = (node.attrs.ctype as ChipType) ?? "node";
  const Icon =
    type === "model"
      ? Box
      : type === "skill"
        ? Lightbulb
        : type === "asset"
          ? Paperclip
          : node.attrs.nodeKind === "group"
            ? Group
            : node.attrs.nodeKind === "character"
              ? UserRound
              : FileText;
  return (
    <NodeViewWrapper as="span" className="inline-block align-baseline">
      <span
        contentEditable={false}
        className={cn(
          "ring-chrome-border mx-0.5 inline-flex h-6 max-w-44 items-center gap-1 rounded-md px-1.5 align-middle text-xs ring-1",
          // 从浅色占位变深的 chip 原地换上，不播入场动画，免得「变深」看着像又冒出一个
          !node.attrs.quiet && "animate-in fade-in-0 zoom-in-90 duration-120",
          CHIP_TONE[type],
        )}
      >
        <Icon className="size-3 shrink-0 opacity-70" />
        <span className="truncate">{String(node.attrs.label ?? "")}</span>
      </span>
    </NodeViewWrapper>
  );
}

/** 浅色占位的图标（lucide 的 file-text / group / user-round 的路径）：占位是装饰层的原生 DOM，不能用 React 图标组件 */
const GHOST_ICON = {
  node: '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M10 9H8"/><path d="M16 13H8"/><path d="M16 17H8"/>',
  group:
    '<path d="M3 7V5c0-1.1.9-2 2-2h2"/><path d="M17 3h2c1.1 0 2 .9 2 2v2"/><path d="M21 17v2c0 1.1-.9 2-2 2h-2"/><path d="M7 21H5c-1.1 0-2-.9-2-2v-2"/><rect width="7" height="5" x="7" y="7" rx="1"/><rect width="7" height="5" x="10" y="12" rx="1"/>',
  character: '<circle cx="12" cy="8" r="5"/><path d="M20 21a8 8 0 0 0-16 0"/>',
} as const;

/** 浅色占位：和深色 chip 同尺寸，虚线边、字色变淡；不可聚焦、不响应点击 */
function ghostDom(ref: SelectionRef) {
  const el = document.createElement("span");
  el.contentEditable = "false";
  el.className =
    "border-foreground/25 text-foreground/45 mx-0.5 inline-flex h-6 max-w-44 items-center gap-1 rounded-md border border-dashed px-1.5 align-middle text-xs select-none animate-in fade-in-0 zoom-in-95 duration-120 motion-reduce:animate-none";
  const icon = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  icon.setAttribute("viewBox", "0 0 24 24");
  icon.setAttribute("class", "size-3 shrink-0 opacity-50");
  icon.setAttribute("fill", "none");
  icon.setAttribute("stroke", "currentColor");
  icon.setAttribute("stroke-width", "2");
  icon.setAttribute("stroke-linecap", "round");
  icon.setAttribute("stroke-linejoin", "round");
  icon.innerHTML = GHOST_ICON[ref.kind === "group" || ref.kind === "character" ? ref.kind : "node"];
  const label = document.createElement("span");
  label.className = "truncate";
  label.textContent = ref.label;
  el.append(icon, label);
  return el;
}

/** 占位插件的状态：占位画在哪（文档位置，null 表示末尾）、画哪些 */
type GhostState = { pos: number | null; refs: SelectionRef[] };

const ghostKey = new PluginKey<GhostState>("selectionGhost");

/** 占位的落点：记住的光标位置，没有（输入框没聚焦过）就在末尾；夹在文档范围内 */
function ghostPos(state: EditorState, pos: number | null) {
  const end = state.doc.content.size - 1;
  return Math.max(1, Math.min(pos ?? end, end));
}

/**
 * 画布选中节点时在输入框光标处出现的浅色占位（widget decoration，不进文档：不影响消息文本、撤销栈和输入法组字）。
 * 失焦时记下光标位置，文档变化时位置跟着映射；占位内容由外面通过 meta 推进来。
 */
const SelectionGhost = Extension.create({
  name: "selectionGhost",
  addProseMirrorPlugins() {
    return [
      new Plugin<GhostState>({
        key: ghostKey,
        state: {
          init: () => ({ pos: null, refs: [] }),
          apply: (tr, prev) => {
            const meta = tr.getMeta(ghostKey) as Partial<GhostState> | undefined;
            const pos =
              meta && "pos" in meta
                ? (meta.pos ?? null)
                : prev.pos === null
                  ? null
                  : tr.mapping.map(prev.pos);
            return { pos, refs: meta?.refs ?? prev.refs };
          },
        },
        props: {
          decorations: (state) => {
            const { pos, refs } = ghostKey.getState(state) ?? { pos: null, refs: [] };
            if (refs.length === 0) return null;
            const at = ghostPos(state, pos);
            return DecorationSet.create(
              state.doc,
              refs.map((ref, i) =>
                Decoration.widget(at, () => ghostDom(ref), {
                  key: `ghost:${ref.id}:${ref.label}`,
                  // 同一个位置上按顺序排，都在光标后面
                  side: i + 1,
                  ignoreSelection: true,
                  stopEvent: () => true,
                }),
              ),
            );
          },
          handleDOMEvents: {
            blur: (view) => {
              view.dispatch(
                view.state.tr
                  .setMeta(ghostKey, { pos: view.state.selection.from })
                  .setMeta("addToHistory", false),
              );
              return false;
            },
          },
        },
      }),
    ];
  },
});

/** 文档里已经有的节点 chip 的 id（浅色占位要避开它们，不重复引用） */
function committedNodeIds(state: EditorState) {
  const ids: string[] = [];
  state.doc.descendants((node) => {
    if (node.type.name === "mention" && (node.attrs.ctype ?? "node") === "node") {
      ids.push(String(node.attrs.id));
    }
  });
  return ids;
}

/** @ 菜单的状态：关键词、光标位置、选中后写入文档的回调 */
type MenuState = { query: string; rect: DOMRect | null; command: SuggestionProps["command"] };

const MAX_MENU_ITEMS = 8;

/** / 菜单的状态；选中后写入文档的回调收一个技能 chip */
type SkillMenuState = MenuState;

/**
 * 输入 / 唤起技能列表：选中后把「/关键词」换成技能 chip。
 * 沿用 suggestion 的默认前缀规则（行首或空格后才触发），所以 http://、和/或 这类斜杠不会误弹。
 */
function skillSuggestion(onChange: (state: SkillMenuState | null) => void) {
  const show = (props: SuggestionProps) =>
    onChange({
      query: props.query,
      rect: props.clientRect?.() ?? null,
      command: props.command,
    });
  return Extension.create({
    name: "skillSuggestion",
    addProseMirrorPlugins() {
      return [
        Suggestion<Chip, Chip>({
          editor: this.editor,
          pluginKey: new PluginKey("skillSuggestion"),
          char: "/",
          items: () => [],
          command: ({ editor, range, props }) => {
            editor
              .chain()
              .focus()
              .insertContentAt(range, [chipNode(props), { type: "text", text: " " }])
              .run();
          },
          render: () => ({
            onStart: show,
            onUpdate: show,
            onExit: () => onChange(null),
          }),
        }),
      ];
    },
  });
}

/** 菜单里的上下键与确认键：返回是否已处理 */
function handleMenuKey(
  event: KeyboardEvent,
  count: number,
  active: number,
  setActive: (i: number) => void,
  pick: (i: number) => void,
) {
  if (count === 0) return false;
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    setActive((active + (event.key === "ArrowDown" ? 1 : count - 1)) % count);
    return true;
  }
  if (event.key === "Enter" || event.key === "Tab") {
    pick(active);
    return true;
  }
  return false;
}

/**
 * 输入区的编辑器：文字 + 行内 chip（节点、模型、技能、附件）。Enter 发送，Shift+Enter 换行，
 * @ 弹出节点列表（向上），输入法选字时的回车不算发送。文本形式见 editor-doc。
 */
export function AgentEditor({
  handleRef,
  nodes,
  selection,
  refNodes,
  placeholder,
  onChange,
  onSubmit,
  onImages,
}: {
  handleRef: Ref<AgentEditorHandle>;
  /** 画布上能 @ 引用的节点 */
  nodes: NodeOption[];
  /** 画布上选中的节点 id（含组）：在光标处显示浅色占位，输入框激活后变成真正的引用 */
  selection: readonly string[];
  /** 解析选中节点标题用的列表（含组） */
  refNodes: readonly RefNode[];
  placeholder: string;
  /** 内容变了：消息文本 */
  onChange: (text: string) => void;
  /** 按了 Enter */
  onSubmit: () => void;
  /** 粘贴或拖入了图片：调用方把它们落到画布并作为 chip 插回来；不传则走默认行为 */
  onImages?: (files: File[]) => void;
}) {
  const [menu, setMenu] = useState<MenuState | null>(null);
  const onImagesRef = useRef(onImages);
  onImagesRef.current = onImages;
  /** 编辑器的 paste / drop 钩子只在创建时绑一次，经 ref 拿最新的回调；返回 true 表示已处理 */
  const attachImages = (files: FileList | null | undefined) => {
    const handler = onImagesRef.current;
    const { images, skipped, clipped } = pickImageFiles(files);
    if (!handler || images.length === 0) return false;
    if (clipped > 0)
      toast.info(`一次最多附 ${MAX_ATTACH_IMAGES} 张图，多出的 ${clipped} 张没放进来`);
    else if (skipped > 0) toast.info(`只能附图片，忽略了 ${skipped} 个其他文件`);
    handler(images);
    return true;
  };
  const [active, setActive] = useState(0);
  const [skillMenu, setSkillMenu] = useState<SkillMenuState | null>(null);
  const [skillActive, setSkillActive] = useState(0);
  const catalog = useSkillCatalog(skillMenu !== null);
  const [empty, setEmpty] = useState(true);
  // 文档里已有的节点 chip（换行拼成字符串，没变化时 setState 不触发重渲染）
  const [committedKey, setCommittedKey] = useState("");
  // 用户关掉的占位：引用了又删掉（退格、撤销、发送后清空）、画布上仍然选着的节点。
  // 占位只在刚选中时出现一次，关掉后不能再冒出来，否则删不掉；取消选中后清掉，再选中重新出一次
  const [dismissed, setDismissed] = useState<string[]>([]);
  const committedIds = useMemo(
    () => (committedKey ? committedKey.split("\n") : []),
    [committedKey],
  );
  // 画布选中变了：取消选中的节点从"已关闭"里去掉（渲染期间同步算，不用 effect，免得多渲染一帧闪出占位）
  const [seenSelection, setSeenSelection] = useState(selection);
  if (seenSelection !== selection) {
    setSeenSelection(selection);
    setDismissed((d) => {
      const next = nextDismissed(d, selection, committedIds, committedIds);
      return next.join("\n") === d.join("\n") ? d : next;
    });
  }
  // 浅色占位 = 画布选中、文档里还没有、也没被关掉的节点
  const pending = useMemo(
    () => pendingRefs(selection, refNodes, committedIds, dismissed),
    [selection, refNodes, committedIds, dismissed],
  );
  // 文档变化的回调挂在编辑器创建时，要读最新的选中和上一次的 chip，所以放进 ref
  const selectionRef = useRef(selection);
  const committedRef = useRef<string[]>([]);
  const pendingKey = pending.map((r) => `${r.id}\u0001${r.kind}\u0001${r.label}`).join("\n");
  const pendingRef = useRef(pending);
  // 键盘处理挂在编辑器创建时，要读最新值，所以放进 ref
  const live = useRef({
    menu,
    active,
    items: [] as NodeOption[],
    skillMenu,
    skillActive,
    skillItems: [] as AgentSkillDto[],
    onSubmit,
  });
  const items = menu
    ? nodes
        .filter((n) => n.label.toLowerCase().includes(menu.query.toLowerCase()))
        .slice(0, MAX_MENU_ITEMS)
    : [];
  const skillItems = skillMenu ? filterSkills(catalog.skills, skillMenu.query) : [];
  useEffect(() => {
    live.current = { menu, active, items, skillMenu, skillActive, skillItems, onSubmit };
  });

  const editor = useEditor({
    immediatelyRender: true,
    extensions: [
      Document,
      Paragraph,
      Text,
      HardBreak,
      UndoRedo,
      SelectionGhost,
      skillSuggestion((state) => {
        setSkillMenu(state);
        if (state) setSkillActive(0);
      }),
      Mention.extend({
        addAttributes: () => ({
          id: { default: "" },
          label: { default: "" },
          ctype: { default: "node" },
          /** 从浅色占位变深的 chip：原地换上，不播入场动画。不进 DOM 属性，也不影响消息文本 */
          quiet: { default: false, rendered: false },
          /** 引用的是组或角色卡（"group" / "character"）：图标跟着换，其余节点用默认图标 */
          nodeKind: { default: null, rendered: false },
        }),
        addNodeView: () => ReactNodeViewRenderer(ChipView, { as: "span" }),
      }).configure({
        renderText: ({ node }) => String(node.attrs.label ?? ""),
        deleteTriggerWithBackspace: true,
        suggestion: {
          char: "@",
          // 中文前面没有空格，@ 紧跟在字后面也要能触发
          allowedPrefixes: null,
          items: () => [],
          render: () => {
            const show = (props: SuggestionProps) => {
              setMenu({
                query: props.query,
                rect: props.clientRect?.() ?? null,
                command: props.command,
              });
              setActive(0);
            };
            return { onStart: show, onUpdate: show, onExit: () => setMenu(null) };
          },
        },
      }),
    ],
    editorProps: {
      attributes: {
        role: "textbox",
        "aria-multiline": "true",
        "aria-label": "给 Agent 的消息",
        class:
          "min-h-[60px] max-h-[140px] overflow-y-auto px-1 text-[13.5px] leading-[1.75] outline-none whitespace-pre-wrap break-words",
      },
      handleKeyDown: (_view, event) => {
        const cur = live.current;
        const { menu: m, items: list, skillMenu: sm, skillItems: skills, onSubmit: submit } = cur;
        if (
          m &&
          handleMenuKey(event, list.length, cur.active, setActive, (i) =>
            m.command({ id: list[i].id, label: list[i].label, ctype: "node" } as never),
          )
        ) {
          return true;
        }
        if (
          sm &&
          handleMenuKey(event, skills.length, cur.skillActive, setSkillActive, (i) =>
            sm.command({ type: "skill", id: skills[i].name, name: skills[i].title } as never),
          )
        ) {
          return true;
        }
        if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
          event.preventDefault();
          submit();
          return true;
        }
        return false;
      },
      // 粘贴 / 拖入图片：交给 onImages 走「上传图片」同一条路；没有图片时放行，文字照常粘进来
      handlePaste: (_view, event) => attachImages(event.clipboardData?.files),
      handleDrop: (_view, event) => attachImages((event as DragEvent).dataTransfer?.files),
    },
    onUpdate: ({ editor: e }) => {
      setEmpty(e.isEmpty);
      onChange(docToMessage(e.getJSON() as DocNode));
    },
    onTransaction: ({ editor: e }) => {
      const next = committedNodeIds(e.state);
      const before = committedRef.current;
      committedRef.current = next;
      setCommittedKey(next.join("\n"));
      // 有 chip 消失（而节点还选着）就把它的占位关掉；没变化时返回原数组，不触发重渲染
      setDismissed((d) => {
        const closed = nextDismissed(d, selectionRef.current, before, next);
        return closed.join("\n") === d.join("\n") ? d : closed;
      });
    },
    // 输入框激活：浅色占位原地变深，成为真正的引用，光标落在它们后面。
    // 点击会在 focus 之后再落光标，所以等一帧再写，免得被点击的光标位置盖掉
    onFocus: ({ editor: e }) => {
      requestAnimationFrame(() => {
        if (e.isDestroyed || !e.isFocused) return;
        const ghost = ghostKey.getState(e.state);
        const content = ghostContent(pendingRef.current).map((n) =>
          n.type === "mention" ? { ...n, attrs: { ...n.attrs, quiet: true } } : n,
        );
        if (!ghost || content.length === 0) return;
        e.chain()
          .command(({ tr }) => {
            tr.setMeta(ghostKey, { refs: [] });
            return true;
          })
          .insertContentAt(ghostPos(e.state, ghost.pos), content)
          .run();
        // 占位已经变成真正的引用：清掉待推的占位，免得同一帧里还没执行的延后推送把它又画回来
        pendingRef.current = [];
      });
    },
  });

  useEffect(() => {
    selectionRef.current = selection;
  }, [selection]);

  // 占位内容跟着画布选中走：推给装饰层，不进撤销栈。
  // 延后一帧再推：点画布节点时，选中更新和输入框失焦几乎同时发生，失焦才会记下光标位置。
  // 立刻推的话占位先画在旧位置、失焦后又搬到光标处，DOM 重建、入场动画播两遍，看起来闪两下；
  // 等一帧，位置记好了再一次画对。选中在这一帧里连续变（先取消再选中）也只画最后一次
  useEffect(() => {
    pendingRef.current = pending;
    if (!editor || editor.isDestroyed) return;
    const frame = requestAnimationFrame(() => {
      if (editor.isDestroyed) return;
      editor.view.dispatch(
        editor.state.tr
          .setMeta(ghostKey, { refs: pendingRef.current })
          .setMeta("addToHistory", false),
      );
    });
    return () => cancelAnimationFrame(frame);
    // pending 每次渲染都是新数组，用内容拼的 key 判断有没有真的变
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [editor, pendingKey]);

  useImperativeHandle(
    handleRef,
    () => ({
      insertChip: (chip) => {
        editor
          ?.chain()
          .focus()
          .insertContent([chipNode(chip), { type: "text", text: " " }])
          .run();
      },
      insertText: (text) => {
        editor?.chain().focus().insertContent(text).run();
      },
      setText: (text) => {
        editor?.chain().setContent(messageToDoc(text)).focus("end").run();
      },
      clear: () => {
        editor?.commands.clearContent(true);
      },
      focus: () => {
        editor?.commands.focus();
      },
    }),
    [editor],
  );

  return (
    <div className="relative">
      {empty && pending.length === 0 && (
        <span className="text-muted-foreground/70 pointer-events-none absolute top-0 left-1 text-[13.5px] leading-[1.75]">
          {placeholder}
        </span>
      )}
      <EditorContent editor={editor} />
      <SuggestionMenu
        label="引用节点"
        rect={menu?.rect ?? null}
        width={240}
        items={items.map((n) => ({ key: n.id, icon: KIND_ICON[n.kind], title: n.label }))}
        active={active}
        onActive={setActive}
        onPick={(i) =>
          menu?.command({ id: items[i].id, label: items[i].label, ctype: "node" } as never)
        }
      />
      <SuggestionMenu
        label="插入技能"
        rect={skillMenu?.rect ?? null}
        width={300}
        items={skillItems.map((k) => ({
          key: k.name,
          icon: Lightbulb,
          title: k.title,
          hint: k.description,
        }))}
        active={skillActive}
        notice={skillMenuNotice(catalog.status, catalog.skills.length, skillItems.length)}
        onActive={setSkillActive}
        onPick={(i) =>
          skillMenu?.command({
            type: "skill",
            id: skillItems[i].name,
            name: skillItems[i].title,
          } as never)
        }
      />
    </div>
  );
}
