import { FileArchive, FileText, FolderOpen, Loader2, UploadCloud } from "lucide-react";
import { motion } from "motion/react";
import { useRef, type ChangeEvent, type CSSProperties } from "react";

import { Notice } from "@/components/admin-ui/notice";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { DURATION, EASE_OUT_CSS, ms, TAP } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { IMPORT_LIMITS, type ImportState } from "@/utils/admin/agent-skill-import";

import { toPicked } from "./drop-files";
import type { PickedItem } from "./use-skill-import";

/** <input webkitdirectory> 不在 React 的类型里，用展开写入 */
const DIRECTORY_ATTRS = { webkitdirectory: "", directory: "" } as Record<string, string>;

const MB = 1048576;

/** 拖放区高亮的过渡：时长与曲线来自 lib/motion */
const MOTION_VARS = {
  "--motion-base": ms(DURATION.base),
  "--motion-ease": EASE_OUT_CSS,
} as CSSProperties;

/**
 * 导入第 1 步：拖放区 + 三个选择按钮 + 限额标签；上传 / 检查中换成进度条与“取消”。
 * 失败原因（超限、不是有效 zip、确认时暂存过期等）以红色提示留在拖放区，并给出下一步。
 * 拖放本身由页面根节点的 dropzone 接收（拖到页面任意位置都行），这里只负责按钮和高亮。
 * @param state 导入状态机的当前状态（pick 或 uploading）
 * @param dragActive 页面上正拖着文件，拖放区高亮
 * @param onPick 选好文件（来自按钮）
 * @param onCancel 取消上传
 */
export function ImportPick({
  state,
  dragActive,
  onPick,
  onCancel,
}: {
  state: Extract<ImportState, { step: "pick" | "uploading" }>;
  dragActive: boolean;
  onPick: (items: PickedItem[]) => void;
  onCancel: () => void;
}) {
  const zipRef = useRef<HTMLInputElement>(null);
  const dirRef = useRef<HTMLInputElement>(null);
  const mdRef = useRef<HTMLInputElement>(null);

  /** 选完就清空 value，同一个文件再选一次也能触发 change */
  const onChange = (event: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(event.target.files ?? []);
    event.target.value = "";
    if (files.length > 0) onPick(toPicked(files));
  };

  if (state.step === "uploading") {
    const percent = Math.round(state.progress * 100);
    return (
      <div
        data-slot="import-progress"
        className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3.5 p-5"
      >
        <span className="bg-muted text-muted-foreground grid size-12 place-items-center rounded-xl">
          <Loader2 className="size-6 animate-spin motion-reduce:animate-none" />
        </span>
        <p className="max-w-full truncate text-sm font-medium">{state.name}</p>
        <div
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={state.phase === "check" ? undefined : percent}
          aria-label="导入进度"
          className="bg-muted h-1.5 w-80 max-w-[80vw] overflow-hidden rounded-full"
        >
          <motion.div
            className="bg-primary h-full origin-left"
            initial={false}
            animate={{ scaleX: state.progress }}
            transition={{ duration: DURATION.fast, ease: "linear" }}
          />
        </div>
        <p className="text-muted-foreground text-xs tabular-nums">
          {state.phase === "check" ? "正在解压和检查…" : `上传中 ${percent}%`}
        </p>
        <Button variant="outline" size="sm" onClick={onCancel}>
          取消
        </Button>
      </div>
    );
  }

  return (
    <div data-slot="import-pick" className="flex min-h-0 flex-1 flex-col gap-4 overflow-auto p-5">
      <div
        data-drag-active={dragActive || undefined}
        style={MOTION_VARS}
        className={cn(
          "flex min-h-72 flex-1 flex-col items-center justify-center gap-3 rounded-xl border-[1.5px] border-dashed p-6 text-center transition-colors duration-(--motion-base) ease-(--motion-ease)",
          dragActive && "border-primary bg-primary/5",
        )}
      >
        <span className="bg-muted text-muted-foreground grid size-12 place-items-center rounded-xl">
          <UploadCloud className="size-6" />
        </span>
        <div>
          <p className="text-base font-medium">把压缩包、文件夹或 SKILL.md 拖到这里</p>
          <p className="text-muted-foreground mt-1 text-sm">也可以直接拖到页面任意位置</p>
        </div>
        <div className="flex flex-wrap justify-center gap-2">
          <motion.div whileTap={TAP} className="inline-flex">
            <Button variant="outline" onClick={() => zipRef.current?.click()}>
              <FileArchive />
              选择压缩包
            </Button>
          </motion.div>
          <motion.div whileTap={TAP} className="inline-flex">
            <Button variant="outline" onClick={() => dirRef.current?.click()}>
              <FolderOpen />
              选择文件夹
            </Button>
          </motion.div>
          <motion.div whileTap={TAP} className="inline-flex">
            <Button variant="outline" onClick={() => mdRef.current?.click()}>
              <FileText />
              选择 SKILL.md
            </Button>
          </motion.div>
        </div>
        <div className="flex flex-wrap justify-center gap-1.5">
          <Tag className="tabular-nums">压缩包 ≤ {IMPORT_LIMITS.zipBytes / MB} MB</Tag>
          <Tag className="tabular-nums">文件夹 ≤ {IMPORT_LIMITS.folderBytes / MB} MB</Tag>
          <Tag className="tabular-nums">≤ {IMPORT_LIMITS.files} 个文件</Tag>
          <Tag className="tabular-nums">单文件 ≤ {IMPORT_LIMITS.fileBytes / MB} MB</Tag>
        </div>
        {state.notice && (
          <Notice tone="danger" className="mt-1 max-w-xl text-left" title="无法导入">
            {state.notice}
          </Notice>
        )}
      </div>

      <input ref={zipRef} type="file" accept=".zip,application/zip" hidden onChange={onChange} />
      <input ref={dirRef} type="file" multiple hidden onChange={onChange} {...DIRECTORY_ATTRS} />
      <input ref={mdRef} type="file" accept=".md,text/markdown" hidden onChange={onChange} />
    </div>
  );
}
