import { memo } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";

import { cn } from "@/lib/utils";

/**
 * 助手消息的 Markdown 渲染。
 * react-markdown 默认不渲染原始 HTML，模型输出里的 script / 事件属性不会生效；
 * remark-gfm 补表格、删除线、任务列表。样式全部走 token，间距取 4 的倍数。
 */
const COMPONENTS: Components = {
  p: ({ node: _n, ...p }) => <p className="my-1.5 first:mt-0 last:mb-0" {...p} />,
  strong: ({ node: _n, ...p }) => <strong className="font-semibold" {...p} />,
  h1: ({ node: _n, ...p }) => <h3 className="mt-3 mb-1.5 text-[15px] font-semibold" {...p} />,
  h2: ({ node: _n, ...p }) => <h3 className="mt-3 mb-1.5 text-[15px] font-semibold" {...p} />,
  h3: ({ node: _n, ...p }) => <h4 className="mt-2.5 mb-1 text-sm font-semibold" {...p} />,
  h4: ({ node: _n, ...p }) => <h4 className="mt-2.5 mb-1 text-sm font-semibold" {...p} />,
  ul: ({ node: _n, ...p }) => <ul className="my-1.5 list-disc space-y-0.5 pl-5" {...p} />,
  ol: ({ node: _n, ...p }) => <ol className="my-1.5 list-decimal space-y-0.5 pl-5" {...p} />,
  li: ({ node: _n, ...p }) => <li className="pl-0.5" {...p} />,
  blockquote: ({ node: _n, ...p }) => (
    <blockquote
      className="border-chrome-border text-muted-foreground my-1.5 border-l-2 pl-3"
      {...p}
    />
  ),
  hr: () => <hr className="border-chrome-border my-3" />,
  a: ({ node: _n, ...p }) => (
    <a
      className="text-primary underline underline-offset-2"
      target="_blank"
      rel="noreferrer noopener"
      {...p}
    />
  ),
  pre: ({ node: _n, ...p }) => (
    <pre
      className="bg-foreground/6 my-2 overflow-x-auto rounded-lg px-3 py-2 text-xs leading-relaxed"
      {...p}
    />
  ),
  code: ({ node: _n, className, ...p }) => (
    <code
      className={cn(
        "font-mono text-[12.5px]",
        // 行内代码没有 language-* 类名；块级代码由外层 pre 提供底色
        !className && "bg-foreground/8 rounded px-1 py-0.5",
        className,
      )}
      {...p}
    />
  ),
  table: ({ node: _n, ...p }) => (
    <div className="my-2 overflow-x-auto">
      <table className="w-full border-collapse text-xs tabular-nums" {...p} />
    </div>
  ),
  th: ({ node: _n, ...p }) => (
    <th className="border-chrome-border border px-2 py-1 text-left font-medium" {...p} />
  ),
  td: ({ node: _n, ...p }) => <td className="border-chrome-border border px-2 py-1" {...p} />,
};

const PLUGINS = [remarkGfm];

/** 流式输出时每个 delta 都会重渲染；text 不变时跳过 */
export const Markdown = memo(function Markdown({ text }: { text: string }) {
  return (
    <ReactMarkdown remarkPlugins={PLUGINS} components={COMPONENTS}>
      {text}
    </ReactMarkdown>
  );
});
