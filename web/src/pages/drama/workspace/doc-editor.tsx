import { useCallback, useEffect, useRef, useState } from "react";

import { TextEditorPane } from "@/components/canvas/text-editor-dialog";
import { Autosaver, type AutosaveStatus } from "@/utils/drama/autosave";

/** 文档区的排版：页面里整篇铺开，不像弹窗那样限高 */
const PAGE_CLASS = "min-h-[50vh] max-h-none overflow-visible px-0";

/**
 * 剧本区的 Markdown 文档编辑器：复用文本节点的 Tiptap Markdown，停手后自动保存，带 revision 乐观锁。
 * - 保存由 onSave 完成，返回保存后的新 revision；撞上 revision 冲突（isConflict）时停止自动保存并汇报 conflict，
 *   由外层弹提示并加载最新（外层换 value / revision 后这里自动重载）；
 * - 外部内容变了（任务写了新版本）：本地没有未保存改动就直接换成新内容，有改动则保留，等保存时由冲突兜底；
 * - 离开（卸载 / 页面隐藏）时立即保存，不丢改动。
 * 只在 docKey 变化时由外层重新挂载（key），所以组件内部可以放心持有编辑状态。
 */
export function DocEditor({
  value,
  revision,
  editable,
  onSave,
  isConflict,
  onStatus,
}: {
  /** 服务端当前的 Markdown */
  value: string;
  /** 服务端当前的 revision */
  revision: number;
  editable: boolean;
  /** 保存一版，返回新 revision */
  onSave: (markdown: string) => Promise<number>;
  isConflict: (error: unknown) => boolean;
  onStatus: (status: AutosaveStatus) => void;
}) {
  /** 编辑器重新挂载的版本号：外部内容替换时加一 */
  const [version, setVersion] = useState(0);
  const [initial, setInitial] = useState(value);
  const latest = useRef({ onSave, isConflict, onStatus });
  latest.current = { onSave, isConflict, onStatus };
  const lastSaved = useRef(value);
  const [saver] = useState(
    () =>
      new Autosaver({
        revision,
        isConflict: (error) => latest.current.isConflict(error),
        save: async (markdown) => {
          const next = await latest.current.onSave(markdown);
          lastSaved.current = markdown;
          return next;
        },
        onStatus: (status) => latest.current.onStatus(status),
      }),
  );

  // 首次载入：以服务端内容为基线
  useEffect(() => {
    saver.reset(value, revision);
    // 只在挂载时执行；之后的外部变化走下面的 effect
    // oxlint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 外部内容变了
  const seen = useRef({ value, revision });
  useEffect(() => {
    if (seen.current.value === value && seen.current.revision === revision) return;
    seen.current = { value, revision };
    // 是自己刚存下去的（或只是别的字段让 revision 前进）：只更新基线，不动编辑器
    if (value === lastSaved.current) {
      saver.reset(lastSaved.current, revision);
      return;
    }
    const status = saver.getStatus();
    if (status === "saved" || status === "conflict") {
      lastSaved.current = value;
      saver.reset(value, revision);
      setInitial(value);
      setVersion((v) => v + 1);
    }
  }, [value, revision, saver]);

  // 离开时立即保存
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

  const onReader = useCallback(() => undefined, []);
  return (
    <TextEditorPane
      key={version}
      text={initial}
      editable={editable}
      autofocus={false}
      onReader={onReader}
      onChange={(markdown) => saver.update(markdown)}
      className={PAGE_CLASS}
    />
  );
}
