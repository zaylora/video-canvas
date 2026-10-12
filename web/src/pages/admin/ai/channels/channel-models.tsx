import { useEffect, useState } from "react";
import { Plus } from "lucide-react";

import { listModels } from "@/api/admin/ai";
import type { ChannelOption, ChannelView, ModelListItem } from "@/api/admin/ai/type.d";
import { DataTablePagination } from "@/components/admin-ui/data-table-pagination";
import { Segmented, SegmentedItem } from "@/components/admin-ui/segmented";
import { Button } from "@/components/ui/button";
import { MODEL_KIND_LABEL } from "@/utils/admin/model-body";

import { KIND_ORDER } from "../kind";
import { ModelRows } from "../models/model-rows";
import type { LoadStatus } from "../../use-admin";

/** 渠道弹窗“模型”页签里对模型的操作（页面持有；新建、编辑在渠道页就地开模型弹窗，测试跳到模型页） */
export type ChannelModelActions = {
  /** 正在切换上线的模型 */
  busyKey: string | null;
  onEdit: (key: string, channel: string) => void;
  onTest: (key: string) => void;
  onNew: (channelKey: string) => void;
  onToggleEnabled: (key: string, enabled: boolean) => void;
  onDelete: (item: ModelListItem) => void;
};

/**
 * 渠道弹窗的“模型”页签：使用这个渠道的模型，按能力筛选，可直接新建、测试、上线。
 * 自己按 channel=<key> 向后端请求模型分页，不依赖全量模型清单。
 * @param channel 当前渠道
 * @param channels 全部渠道（options）：行里显示渠道所属插件
 * @param kinds 渠道所用插件版本支持的生成方式（决定能力筛选里有哪些）
 * @param refreshKey 递增即重新拉取（模型有改动之后）
 * @param onTotal 命中总数变化时回调（页签上的数量角标）
 */
export function ChannelModels({
  channel,
  channels,
  kinds,
  refreshKey,
  actions,
  onTotal,
}: {
  channel: ChannelView;
  channels: ChannelOption[];
  kinds: string[];
  refreshKey: number;
  actions: ChannelModelActions;
  onTotal?: (total: number) => void;
}) {
  const [kind, setKind] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [items, setItems] = useState<ModelListItem[]>([]);
  const [total, setTotal] = useState(0);
  const [status, setStatus] = useState<LoadStatus>("loading");
  const [version, setVersion] = useState(0);
  const key = channel.key;

  useEffect(() => {
    let cancelled = false;
    listModels({ channel: key, kind, page, page_size: pageSize })
      .then((result) => {
        if (cancelled) return;
        setItems(result.list);
        setTotal(result.total);
        setStatus("ready");
        if (!kind) onTotal?.(result.total);
      })
      .catch(() => !cancelled && setStatus((prev) => (prev === "ready" ? prev : "error")));
    return () => {
      cancelled = true;
    };
    // onTotal 由调用方保证稳定；它变化不该重新请求
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, kind, page, pageSize, refreshKey, version]);

  const shownKinds = KIND_ORDER.filter((item) => kinds.length === 0 || kinds.includes(item));
  const pick = (next: string) => {
    setKind(next);
    setPage(1);
  };

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <Segmented aria-label="按能力筛选">
          <SegmentedItem slideId="channel-models-kind" active={!kind} onClick={() => pick("")}>
            全部
          </SegmentedItem>
          {shownKinds.map((item) => (
            <SegmentedItem
              key={item}
              slideId="channel-models-kind"
              active={kind === item}
              onClick={() => pick(item)}
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
        models={items}
        status={status}
        channels={channels}
        hideChannel
        busyKey={actions.busyKey}
        empty="还没有模型使用这个渠道"
        onEdit={(modelKey) => actions.onEdit(modelKey, channel.key)}
        onTest={actions.onTest}
        onToggleEnabled={actions.onToggleEnabled}
        onDelete={actions.onDelete}
        onRetry={() => setVersion((v) => v + 1)}
      />
      {status === "ready" && total > 0 && (
        <DataTablePagination
          page={page}
          pageSize={pageSize}
          total={total}
          onPageChange={setPage}
          onPageSizeChange={(size) => {
            setPageSize(size);
            setPage(1);
          }}
        />
      )}
    </div>
  );
}
