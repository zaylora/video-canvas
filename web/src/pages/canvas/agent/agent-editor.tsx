import { useEffect, useImperativeHandle, useRef, useState, type Ref } from "react";
import { createPortal } from "react-dom";
import {
  Box,
  Clapperboard,
  FileText,
  Image as ImageIcon,
  Music,
  Paperclip,
  Sparkles,
} from "lucide-react";
import { motion, useReducedMotion } from "motion/react";
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
} from "@tiptap/react";
import type { SuggestionProps } from "@tiptap/suggestion";

import { DURATION } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { Chip, ChipType } from "@/utils/agent/chips";
import { chipNode, docToMessage, messageToDoc, type DocNode } from "@/utils/agent/editor-doc";

/** 能用 @ 引用的节点 */
export type NodeOption = {
  /** 节点 id */
  id: string;
  /** 标题 */
  label: string;
  /** 节点种类 */
  kind: "script" | "image" | "video" | "audio";
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
} as const;

/** 行内 chip：不可编辑的原子节点，退格整个删除 */
function ChipView({ node }: NodeViewProps) {
  const type = (node.attrs.ctype as ChipType) ?? "node";
  const Icon =
    type === "model" ? Box : type === "skill" ? Sparkles : type === "asset" ? Paperclip : FileText;
  return (
    <NodeViewWrapper as="span" className="inline-block align-baseline">
      <span
        contentEditable={false}
        className={cn(
          "ring-chrome-border mx-0.5 inline-flex h-6 max-w-44 items-center gap-1 rounded-md px-1.5 align-middle text-xs ring-1",
          "animate-in fade-in-0 zoom-in-90 duration-120",
          CHIP_TONE[type],
        )}
      >
        <Icon className="size-3 shrink-0 opacity-70" />
        <span className="truncate">{String(node.attrs.label ?? "")}</span>
      </span>
    </NodeViewWrapper>
  );
}

/** @ 菜单的状态：关键词、光标位置、选中后写入文档的回调 */
type MenuState = { query: string; rect: DOMRect | null; command: SuggestionProps["command"] };

const MAX_MENU_ITEMS = 8;

/**
 * 输入区的编辑器：文字 + 行内 chip（节点、模型、技能、附件）。Enter 发送，Shift+Enter 换行，
 * @ 弹出节点列表（向上），输入法选字时的回车不算发送。文本形式见 editor-doc。
 */
export function AgentEditor({
  handleRef,
  nodes,
  placeholder,
  onChange,
  onSubmit,
}: {
  handleRef: Ref<AgentEditorHandle>;
  /** 画布上能引用的节点 */
  nodes: NodeOption[];
  placeholder: string;
  /** 内容变了：消息文本 */
  onChange: (text: string) => void;
  /** 按了 Enter */
  onSubmit: () => void;
}) {
  const reduce = useReducedMotion();
  const [menu, setMenu] = useState<MenuState | null>(null);
  const [active, setActive] = useState(0);
  const [empty, setEmpty] = useState(true);
  // 键盘处理挂在编辑器创建时，要读最新值，所以放进 ref
  const live = useRef({ menu, active, items: [] as NodeOption[], onSubmit });
  const items = menu
    ? nodes
        .filter((n) => n.label.toLowerCase().includes(menu.query.toLowerCase()))
        .slice(0, MAX_MENU_ITEMS)
    : [];
  useEffect(() => {
    live.current = { menu, active, items, onSubmit };
  });

  const editor = useEditor({
    immediatelyRender: true,
    extensions: [
      Document,
      Paragraph,
      Text,
      HardBreak,
      UndoRedo,
      Mention.extend({
        addAttributes: () => ({
          id: { default: "" },
          label: { default: "" },
          ctype: { default: "node" },
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
        const { menu: m, active: a, items: list, onSubmit: submit } = live.current;
        if (m && list.length > 0) {
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            setActive((a + (event.key === "ArrowDown" ? 1 : list.length - 1)) % list.length);
            return true;
          }
          if (event.key === "Enter" || event.key === "Tab") {
            const pick = list[a];
            m.command({ id: pick.id, label: pick.label, ctype: "node" } as never);
            return true;
          }
        }
        if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
          event.preventDefault();
          submit();
          return true;
        }
        return false;
      },
    },
    onUpdate: ({ editor: e }) => {
      setEmpty(e.isEmpty);
      onChange(docToMessage(e.getJSON() as DocNode));
    },
  });

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
      {empty && (
        <span className="text-muted-foreground/70 pointer-events-none absolute top-0 left-1 text-[13.5px] leading-[1.75]">
          {placeholder}
        </span>
      )}
      <EditorContent editor={editor} />
      {menu?.rect &&
        items.length > 0 &&
        createPortal(
          <motion.ul
            role="listbox"
            aria-label="引用节点"
            initial={reduce ? { opacity: 0 } : { opacity: 0, y: 4, scale: 0.96 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            transition={{ duration: DURATION.base }}
            style={{
              position: "fixed",
              left: Math.max(8, Math.min(menu.rect.left, window.innerWidth - 248)),
              bottom: window.innerHeight - menu.rect.top + 6,
              transformOrigin: "bottom left",
            }}
            className="bg-popover text-popover-foreground ring-chrome-border z-[60] flex w-60 flex-col gap-0.5 rounded-xl p-1.5 shadow-lg ring-1"
          >
            {items.map((n, i) => {
              const Icon = KIND_ICON[n.kind];
              return (
                <li key={n.id} role="option" aria-selected={i === active}>
                  <button
                    type="button"
                    // 按下时编辑器不要失焦，否则菜单先关了
                    onMouseDown={(e) => e.preventDefault()}
                    onMouseEnter={() => setActive(i)}
                    onClick={() =>
                      menu.command({ id: n.id, label: n.label, ctype: "node" } as never)
                    }
                    className={cn(
                      "flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-[13px]",
                      i === active && "bg-chrome-hover",
                    )}
                  >
                    <Icon className="text-muted-foreground size-4 shrink-0" />
                    <span className="truncate">{n.label}</span>
                  </button>
                </li>
              );
            })}
          </motion.ul>,
          document.body,
        )}
    </div>
  );
}
