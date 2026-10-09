import { useState } from "react";
import { Plus } from "lucide-react";

import type {
  ChannelView,
  ConfigListItem,
  ConfigRevision,
  PluginView,
} from "@/api/admin/ai/type.d";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { Button } from "@/components/ui/button";
import { channelSupportsKind } from "@/utils/admin/plugin";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";

import { KIND_ORDER } from "../kind";
import { ModelRows } from "../models/model-rows";
import type { LoadStatus } from "../../use-admin";

/** 渠道弹窗“模型”页签里对模型的操作（页面持有，和模型页共用同一套） */
export type ChannelModelActions = {
  /** 正在切换上线的模型 */
  busyKey: string | null;
  onEdit: (key: string) => void;
  onTest: (key: string) => void;
  onNew: (channelKey: string) => void;
  onToggleEnabled: (key: string, enabled: boolean) => void;
  onRollback: (key: string, revision: ConfigRevision) => void;
  onDelete: (item: ConfigListItem) => void;
};

/** 渠道弹窗的“模型”页签：使用这个渠道的模型，按能力筛选，可直接新建、测试、上线 */
export function ChannelModels({
  channel,
  channels,
  plugins,
  models,
  status,
  actions,
}: {
  channel: ChannelView;
  channels: ChannelView[];
  plugins: PluginView[];
  models: ConfigListItem[];
  status: LoadStatus;
  actions: ChannelModelActions;
}) {
  const [kind, setKind] = useState("");
  const used = models.filter((item) => item.channel === channel.key);
  const shown = used.filter((item) => !kind || item.kind === kind);
  const kinds = KIND_ORDER.filter((item) => channelSupportsKind(plugins, channel, item) !== false);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <Segmented aria-label="按能力筛选">
          <SegmentedItem slideId="channel-models-kind" active={!kind} onClick={() => setKind("")}>
            全部
          </SegmentedItem>
          {kinds.map((item) => (
            <SegmentedItem
              key={item}
              slideId="channel-models-kind"
              active={kind === item}
              onClick={() => setKind(item)}
            >
              {MODEL_KIND_LABEL[item]}
            </SegmentedItem>
          ))}
        </Segmented>
        <Button
          size="sm"
          variant="outline"
          className="ml-auto"
          onClick={() => actions.onNew(channel.key)}
        >
          <Plus />
          新建模型
        </Button>
      </div>
      <ModelRows
        models={shown}
        status={status}
        channels={channels}
        plugins={plugins}
        hideChannel
        busyKey={actions.busyKey}
        empty="还没有模型使用这个渠道"
        onEdit={actions.onEdit}
        onTest={actions.onTest}
        onToggleEnabled={actions.onToggleEnabled}
        onRollback={actions.onRollback}
        onDelete={actions.onDelete}
      />
    </div>
  );
}
