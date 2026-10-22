import { useCallback, useEffect, useRef, type MouseEvent, type ReactNode } from "react";
import { EditorContent, useEditor, type Editor } from "@tiptap/react";
import {
  Bold,
  Code,
  Heading1,
  Heading2,
  Heading3,
  Italic,
  List,
  ListOrdered,
  Minus,
  Quote,
  Redo2,
  Strikethrough,
  Undo2,
} from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { canvasDialogGuards } from "@/utils/canvas/dialog-guards";
import { TEXT_BODY_MAX, clipText } from "@/utils/canvas/paste";

import { TEXT_EDITOR_EXTENSIONS } from "./text-editor-extensions";

/** 编辑区的排版：Tailwind 的 preflight 抹掉了标题、列表的默认样式，这里补回来，层级靠字重和字号 */
const EDITOR_CLASS = cn(
  "nowheel min-h-72 max-h-[60vh] overflow-y-auto px-1 text-sm leading-7 break-words whitespace-pre-wrap outline-none select-text",
  "[&_h1]:mt-4 [&_h1]:mb-2 [&_h1]:text-xl [&_h1]:font-semibold",
  "[&_h2]:mt-4 [&_h2]:mb-2 [&_h2]:text-lg [&_h2]:font-semibold",
  "[&_h3]:mt-3 [&_h3]:mb-1.5 [&_h3]:text-base [&_h3]:font-semibold",
  "[&_p]:my-1.5 [&_strong]:font-semibold [&_s]:opacity-70",
  "[&_ul]:my-1.5 [&_ul]:list-disc [&_ul]:pl-6 [&_ol]:my-1.5 [&_ol]:list-decimal [&_ol]:pl-6",
  "[&_blockquote]:border-chrome-border [&_blockquote]:text-muted-foreground [&_blockquote]:my-2 [&_blockquote]:border-l-2 [&_blockquote]:pl-3",
  "[&_hr]:border-chrome-border [&_hr]:my-4",
  "[&_code]:bg-foreground/8 [&_code]:rounded [&_code]:px-1 [&_code]:py-0.5 [&_code]:font-mono [&_code]:text-[12.5px]",
  "[&_pre]:bg-foreground/6 [&_pre]:my-2 [&_pre]:rounded-lg [&_pre]:px-3 [&_pre]:py-2 [&_pre_code]:bg-transparent [&_pre_code]:p-0",
);

/** 工具栏上的一个按钮 */
type ToolbarItem = {
  /** 无障碍名称，同时是悬停提示 */
  label: string;
  /** 按钮图标 */
  icon: ReactNode;
  /** 点击后对编辑器做的事 */
  run: (editor: Editor) => void;
  /** 光标所在处是否已套用这个格式（按钮显示按下态）；没有则恒为未按下 */
  active?: (editor: Editor) => boolean;
  /** 现在能不能点，默认能 */
  enabled?: (editor: Editor) => boolean;
};

/** 工具栏分组：组与组之间画一条竖线 */
const TOOLBAR_GROUPS: ToolbarItem[][] = [
  [
    {
      label: "一级标题",
      icon: <Heading1 />,
      run: (e) => e.chain().focus().toggleHeading({ level: 1 }).run(),
      active: (e) => e.isActive("heading", { level: 1 }),
    },
    {
      label: "二级标题",
      icon: <Heading2 />,
      run: (e) => e.chain().focus().toggleHeading({ level: 2 }).run(),
      active: (e) => e.isActive("heading", { level: 2 }),
    },
    {
      label: "三级标题",
      icon: <Heading3 />,
      run: (e) => e.chain().focus().toggleHeading({ level: 3 }).run(),
      active: (e) => e.isActive("heading", { level: 3 }),
    },
  ],
  [
    {
      label: "加粗",
      icon: <Bold />,
      run: (e) => e.chain().focus().toggleBold().run(),
      active: (e) => e.isActive("bold"),
    },
    {
      label: "斜体",
      icon: <Italic />,
      run: (e) => e.chain().focus().toggleItalic().run(),
      active: (e) => e.isActive("italic"),
    },
    {
      label: "删除线",
      icon: <Strikethrough />,
      run: (e) => e.chain().focus().toggleStrike().run(),
      active: (e) => e.isActive("strike"),
    },
    {
      label: "行内代码",
      icon: <Code />,
      run: (e) => e.chain().focus().toggleCode().run(),
      active: (e) => e.isActive("code"),
    },
  ],
  [
    {
      label: "无序列表",
      icon: <List />,
      run: (e) => e.chain().focus().toggleBulletList().run(),
      active: (e) => e.isActive("bulletList"),
    },
    {
      label: "有序列表",
      icon: <ListOrdered />,
      run: (e) => e.chain().focus().toggleOrderedList().run(),
      active: (e) => e.isActive("orderedList"),
    },
    {
      label: "引用",
      icon: <Quote />,
      run: (e) => e.chain().focus().toggleBlockquote().run(),
      active: (e) => e.isActive("blockquote"),
    },
    {
      label: "分隔线",
      icon: <Minus />,
      run: (e) => e.chain().focus().setHorizontalRule().run(),
    },
  ],
  [
    {
      label: "撤销",
      icon: <Undo2 />,
      run: (e) => e.chain().focus().undo().run(),
      enabled: (e) => e.can().undo(),
    },
    {
      label: "重做",
      icon: <Redo2 />,
      run: (e) => e.chain().focus().redo().run(),
      enabled: (e) => e.can().redo(),
    },
  ],
];

/** 工具栏：按钮按下时不能抢走编辑器的焦点，否则选区丢了格式就套不上 */
function Toolbar({ editor }: { editor: Editor }) {
  const keepFocus = (event: MouseEvent) => event.preventDefault();
  return (
    <div className="border-chrome-border flex flex-wrap items-center gap-x-1 gap-y-1 border-b pb-2">
      {TOOLBAR_GROUPS.map((group, index) => (
        <div key={group[0].label} className="flex items-center gap-0.5">
          {index > 0 && <span className="bg-chrome-border mr-1.5 ml-0.5 h-4 w-px" aria-hidden />}
          {group.map((item) => {
            const pressed = item.active?.(editor) ?? false;
            return (
              <Button
                key={item.label}
                type="button"
                variant="ghost"
                size="icon-sm"
                title={item.label}
                aria-label={item.label}
                aria-pressed={item.active ? pressed : undefined}
                disabled={item.enabled ? !item.enabled(editor) : false}
                className={cn(pressed && "bg-foreground/10")}
                onMouseDown={keepFocus}
                onClick={() => item.run(editor)}
              >
                {item.icon}
              </Button>
            );
          })}
        </div>
      ))}
    </div>
  );
}

/**
 * 弹窗里的富文本编辑区：内容是 Markdown，打字时 `## `、`**加粗**`、`- ` 会即时变成格式。
 * 只在弹窗打开时挂载，画布上的文本节点平时不背编辑器。
 * @param props.text 节点现在的 Markdown 正文，只在挂载时读一次
 * @param props.editable 能否编辑；生成中为 false，只读查看
 * @param props.onReader 挂载后交出「读出当前 Markdown」的函数，卸载时交出 null；关闭弹窗时外层靠它取稿
 */
function TextEditorPane({
  text,
  editable,
  onReader,
}: {
  text: string;
  editable: boolean;
  onReader: (read: (() => string) | null) => void;
}) {
  const editor = useEditor({
    extensions: TEXT_EDITOR_EXTENSIONS,
    content: text,
    contentType: "markdown",
    editable,
    autofocus: editable ? "end" : false,
    // 工具栏要跟着光标所在的格式刷新按下态
    shouldRerenderOnTransaction: true,
    editorProps: {
      attributes: {
        "aria-label": "文本内容",
        "aria-multiline": "true",
        role: "textbox",
        class: EDITOR_CLASS,
      },
    },
  });

  useEffect(() => {
    onReader(editor ? () => editor.getMarkdown() : null);
    return () => onReader(null);
  }, [editor, onReader]);

  // 编辑中途变成不可编辑（比如任务回填触发了生成中）：锁住输入，关闭时也不再保存
  useEffect(() => {
    editor?.setEditable(editable, false);
  }, [editable, editor]);

  if (!editor) return null;
  return (
    <>
      {editable && <Toolbar editor={editor} />}
      <EditorContent editor={editor} />
    </>
  );
}

/**
 * 文本节点的编辑弹窗：居中的富文本框，Esc、点遮罩或右上角关闭时把内容存回节点。
 * 弹窗是 Portal，React 事件仍会冒泡回画布节点，所以点击和按下要拦住，免得选中、拖动了节点。
 * @param props.open 是否打开
 * @param props.onOpenChange 打开状态变化（关闭时为 false）
 * @param props.title 弹窗标题，用节点名
 * @param props.text 节点正文（Markdown）
 * @param props.editable 能否编辑；不能编辑时只读查看，没有工具栏
 * @param props.onSave 关闭时内容有变化才回调，参数是编辑后的 Markdown（已卡在正文上限内）
 */
export function TextEditorDialog({
  open,
  onOpenChange,
  title,
  text,
  editable = false,
  onSave,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  text: string;
  editable?: boolean;
  onSave?: (text: string) => void;
}) {
  const readMarkdown = useRef<(() => string) | null>(null);
  const setReader = useCallback((read: (() => string) | null) => {
    readMarkdown.current = read;
  }, []);

  const handleOpenChange = (next: boolean) => {
    if (!next && editable && onSave && readMarkdown.current) {
      const { text: draft, clipped } = clipText(readMarkdown.current());
      if (clipped) toast.info(`文字太长，只保存了前 ${TEXT_BODY_MAX} 个字`);
      if (draft !== text) onSave(draft);
    }
    onOpenChange(next);
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-4xl" {...canvasDialogGuards}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <TextEditorPane text={text} editable={editable} onReader={setReader} />
      </DialogContent>
    </Dialog>
  );
}
