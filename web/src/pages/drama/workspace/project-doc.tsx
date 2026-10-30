import { useMemo, useState } from "react";
import { FilePenLine, FileText, LayoutGrid } from "lucide-react";

import { cn } from "@/lib/utils";
import { isRevisionConflict } from "@/api/drama/errors";
import { ChromePill, ChromeSegment } from "@/components/canvas/chrome/chrome";
import { parseCharacters } from "@/utils/drama/parse-characters";
import type { ScriptDoc } from "@/utils/drama/stages";

import { AnalyzingView, useAnalyzing } from "./analyzing-view";
import { CharacterCards } from "./character-cards";
import { useWorkspace } from "./context";
import { DocEditor } from "./doc-editor";

/** 人物传记看卡片还是看 / 改原文 */
type CharactersView = "cards" | "source";

const VIEW_ITEMS = [
  { value: "cards" as const, label: "人物卡片", icon: <LayoutGrid className="size-4" /> },
  { value: "source" as const, label: "编辑原文", icon: <FileText className="size-4" /> },
];

/**
 * 稿纸的排版：集名用衬线粗体，场次标题（二到四级标题）是等宽灰底的小标签，
 * 角色名、台词沿用编辑器自带的样式。浅色是米白纸，深色是比底色亮一档的纸。
 */
export const PAPER_PROSE =
  "[&_h1]:font-display [&_h1]:font-black [&_h1]:tracking-wide " +
  "[&_h2]:bg-muted [&_h2]:inline-flex [&_h2]:rounded-md [&_h2]:px-3 [&_h2]:py-1.5 [&_h2]:font-mono [&_h2]:text-[12.5px] [&_h2]:tracking-wider " +
  "[&_h3]:bg-muted [&_h3]:inline-flex [&_h3]:rounded-md [&_h3]:px-3 [&_h3]:py-1.5 [&_h3]:font-mono [&_h3]:text-[12.5px] [&_h3]:tracking-wider";

const TITLES: Record<ScriptDoc, string> = { outline: "故事大纲", characters: "人物传记" };

/**
 * 大纲 / 人物传记：研读时生成，随短剧一起保存（同一个 revision），在剧本页分集栏顶上打开。
 * 研读中还没写出来时显示研读进度，不给编辑，免得手写的内容被生成结果盖掉。
 * 人物传记默认拆成人物卡片看，可切回原文编辑；拆不出人物（写法认不出）时直接显示原文。
 */
export function ProjectDoc({ doc }: { doc: ScriptDoc }) {
  const { drama, patch, reportSave } = useWorkspace();
  const { analyzing } = useAnalyzing();
  const markdown = doc === "outline" ? drama.outlineMd : drama.charactersMd;
  const [view, setView] = useState<CharactersView>("cards");
  const cards = useMemo(
    () => (doc === "characters" ? parseCharacters(markdown) : []),
    [doc, markdown],
  );
  const showCards = cards.length > 0 && view === "cards";
  if (analyzing && !markdown) {
    return (
      <div className="mx-auto w-full max-w-[720px] px-6 pt-6 pb-28 max-sm:px-3">
        <h2 className="font-display text-[22px] font-black tracking-wide">{TITLES[doc]}</h2>
        <AnalyzingView />
      </div>
    );
  }
  if (showCards) {
    return (
      <div className="mx-auto w-full max-w-[1040px] px-6 pt-6 pb-28 max-sm:px-3">
        <div className="flex items-center justify-between gap-3">
          <h2 className="font-display text-[22px] font-black tracking-wide">{TITLES[doc]}</h2>
          <ChromePill>
            <ChromeSegment
              id="characters-view"
              value={view}
              onValueChange={setView}
              items={VIEW_ITEMS}
            />
          </ChromePill>
        </div>
        <CharacterCards cards={cards} />
      </div>
    );
  }
  return (
    <div className="mx-auto w-full max-w-[720px] px-6 pt-6 pb-28 max-sm:px-3">
      <div className="flex items-center justify-between gap-3">
        <h2 className="font-display text-[22px] font-black tracking-wide">{TITLES[doc]}</h2>
        {cards.length > 0 && (
          <ChromePill>
            <ChromeSegment
              id="characters-view"
              value={view}
              onValueChange={setView}
              items={VIEW_ITEMS}
            />
          </ChromePill>
        )}
      </div>
      {!markdown && (
        <div className="text-muted-foreground mt-10 flex flex-col items-center gap-1 text-center text-sm">
          <FilePenLine className="size-7" />
          <div>还没有{TITLES[doc]}</div>
          <div className="text-xs">研读剧本时会自动生成，也可以直接在下面手写</div>
        </div>
      )}
      <div
        className={cn(
          "bg-paper text-paper-foreground ring-border mt-4 rounded-t-md rounded-b-2xl px-[clamp(20px,6vw,72px)] py-12 shadow-[0_24px_60px_-24px_rgb(0_0_0/0.5)] ring-1",
          PAPER_PROSE,
        )}
      >
        <DocEditor
          key={doc}
          value={markdown}
          revision={drama.revision}
          editable
          isConflict={isRevisionConflict}
          onStatus={(status) => reportSave(doc, status)}
          onSave={(next) => patch(doc === "outline" ? { outlineMd: next } : { charactersMd: next })}
        />
      </div>
    </div>
  );
}
