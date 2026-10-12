import { useMemo } from "react";

import type { AdminCatalog } from "../../use-admin";
import { ModelEditorDialogs } from "../models/model-editor-dialogs";
import type { ModelRoute } from "../models/model-route";
import { useModelWorkspace } from "../models/use-model-workspace";

/** 渠道页里正在编辑的模型：key 为 null 是在该渠道下新建 */
export type ChannelModelEditing = { channel: string; key: string | null };

/**
 * 渠道页的模型编辑弹窗：在渠道弹窗的“模型”页签里新建 / 编辑模型时直接在本页打开，不跳到模型页。
 * 选中状态放在渠道页自己的 state 里（不进 URL），渲染在渠道弹窗内部，关掉后渠道弹窗保持原样。
 */
export function ChannelModelEditor({
  catalog,
  editing,
  onChange,
  onClose,
  onListChanged,
}: {
  catalog: AdminCatalog;
  editing: ChannelModelEditing | null;
  onChange: (next: ChannelModelEditing) => void;
  onClose: () => void;
  onListChanged: () => void;
}) {
  const channel = editing?.channel ?? "";
  const route = useMemo<ModelRoute>(
    () => ({
      key: editing?.key ?? null,
      isNew: !!editing && editing.key === null,
      draftId: null,
      fromImport: false,
      presetChannel: editing?.key === null ? editing.channel : null,
      openModel: (key) => onChange({ channel, key }),
      openNew: () => onChange({ channel, key: null }),
      close: () => onClose(),
    }),
    [editing, channel, onChange, onClose],
  );
  const ws = useModelWorkspace(catalog, route, onListChanged);
  return <ModelEditorDialogs ws={ws} catalog={catalog} />;
}
