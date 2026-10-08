import { useEffect, useImperativeHandle, useRef, useState, type Ref } from "react";
import {
  Box,
  Clapperboard,
  FileText,
  Image as ImageIcon,
  Music,
  Paperclip,
  Sparkles,
} from "lucide-react";
import Document from "@tiptap/extension-document";
import HardBreak from "@tiptap/extension-hard-break";
import Mention from "@tiptap/extension-mention";
import Paragraph from "@tiptap/extension-paragraph";
import Text from "@tiptap/extension-text";
import { Extension } from "@tiptap/core";
import { UndoRedo } from "@tiptap/extensions";
import { PluginKey } from "@tiptap/pm/state";
import {
  EditorContent,
  NodeViewWrapper,
  ReactNodeViewRenderer,
  useEditor,
  type NodeViewProps,
} from "@tiptap/react";
import Suggestion, { type SuggestionProps } from "@tiptap/suggestion";

import { cn } from "@/lib/utils";
import type { Chip, ChipType } from "@/utils/agent/chips";
import { chipNode, docToMessage, messageToDoc, type DocNode } from "@/utils/agent/editor-doc";
import type { AgentSkillDto } from "@/api/agent/type.d";
import { filterSkills, skillMenuNotice } from "@/utils/agent/skills";

import { SuggestionMenu } from "./suggestion-menu";
import { useSkillCatalog } from "./use-skill-catalog";

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
  const [menu, setMenu] = useState<MenuState | null>(null);
  const [active, setActive] = useState(0);
  const [skillMenu, setSkillMenu] = useState<SkillMenuState | null>(null);
  const [skillActive, setSkillActive] = useState(0);
  const catalog = useSkillCatalog(skillMenu !== null);
  const [empty, setEmpty] = useState(true);
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
      skillSuggestion((state) => {
        setSkillMenu(state);
        if (state) setSkillActive(0);
      }),
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
          icon: Sparkles,
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
