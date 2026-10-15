import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Loader2, RefreshCw } from "lucide-react";
import { toast } from "sonner";

import { importChannelModels, pullChannelModels } from "@/api/admin/ai";
import type { ChannelOption, PulledModel } from "@/api/admin/ai/type.d";
import { Notice } from "@/components/admin-ui/notice";
import { SearchInput } from "@/components/admin-ui/search-input";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { openDialog, type DialogControl } from "@/store/dialog";
import { errorMessage, isSecretMissing } from "@/utils/admin/errors";
import {
  countPrefixedKeys,
  filterPulled,
  initialSelection,
  selectionStats,
  setSelection,
} from "@/utils/admin/pull-models";

import { useAliveRef } from "../../use-admin";

/** 拉取模型弹窗的业务参数 */
export type PullModelsDialogProps = {
  /** 从哪个渠道拉取 */
  channel: ChannelOption;
  /** 是否 super_admin：决定“Key 未设置”时提示自己设置还是联系 super_admin */
  canWriteChannel: boolean;
  /** 导入成功后（刷新模型表与左栏模型数）；弹窗会自己关闭 */
  onImported: () => void;
  /** 点“手动新增模型”：弹窗关闭后触发；不传则只提示去点“新增模型” */
  onManualAdd?: () => void;
};

/** 还没拿到上游清单时用的空数组：引用稳定，免得每次渲染都重算 */
const NO_MODELS: PulledModel[] = [];

/** 拉取的状态：加载中、失败（带原因与是否缺 Key）、成功（带上游模型） */
type PullState =
  | { kind: "loading" }
  | { kind: "error"; message: string; secretMissing: boolean }
  | { kind: "ready"; models: PulledModel[] };

/**
 * 拉取模型弹窗：打开即向上游请求模型清单（宿主内置的通用请求 GET {base_url}/v1/models，不落库）。
 * - 成功：默认全选，可搜索、批量全选 / 取消全选（只作用于搜索命中的项）；已存在的模型会标出，勾选了也不会重复导入；
 * - 失败：显示后端原因与建议，可重试或手动新增模型；Key 未设置时按角色提示（super_admin 去设置 Key，admin 联系 super_admin）；
 * - 上游返回空列表：提示手动新增；
 * - 确认导入：把勾选的全部 id 交给后端，后端跳过已有、新建“待配置”模型。
 */
export function PullModelsDialog({
  channel,
  canWriteChannel,
  onImported,
  onManualAdd,
  open,
  onClose,
  onExited,
}: PullModelsDialogProps & DialogControl) {
  const aliveRef = useAliveRef();
  const [state, setState] = useState<PullState>({ kind: "loading" });
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [query, setQuery] = useState("");
  const [importing, setImporting] = useState(false);
  /** 最近一次拉取的序号：只认最新那次，重试时旧请求的结果丢弃 */
  const seq = useRef(0);

  const pull = useCallback(async () => {
    const id = ++seq.current;
    setState({ kind: "loading" });
    try {
      const result = await pullChannelModels(channel.key);
      if (!aliveRef.current || id !== seq.current) return;
      setSelected(initialSelection(result.models));
      setQuery("");
      setState({ kind: "ready", models: result.models });
    } catch (cause) {
      // 全局 toast 已弹；把原因和下一步建议留在弹窗里
      if (!aliveRef.current || id !== seq.current) return;
      setState({
        kind: "error",
        message: errorMessage(cause, "向上游请求模型列表失败"),
        secretMissing: isSecretMissing(cause),
      });
    }
  }, [aliveRef, channel.key]);

  useEffect(() => {
    void pull();
  }, [pull]);

  const models = state.kind === "ready" ? state.models : NO_MODELS;
  const stats = useMemo(() => selectionStats(models, selected), [models, selected]);
  const visible = useMemo(() => filterPulled(models, query), [models, query]);

  const manualAdd = () => {
    onClose();
    if (onManualAdd) onManualAdd();
    else toast.info("请点击“新增模型”手动添加");
  };

  const confirmImport = async () => {
    if (importing || stats.newCount === 0) return;
    setImporting(true);
    try {
      const result = await importChannelModels(channel.key, [...selected]);
      if (!aliveRef.current) return;
      const prefixed = countPrefixedKeys(channel.key, result.created);
      toast.success(
        `已导入 ${result.created.length} 个待配置模型${
          prefixed ? `（${prefixed} 个 key 与其他模型冲突，已加渠道前缀）` : ""
        }`,
      );
      onImported();
      onClose();
    } catch {
      // 全局 toast 已弹，保留选择让用户重试
    } finally {
      if (aliveRef.current) setImporting(false);
    }
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && !importing && onClose()}
      onOpenChangeComplete={(next) => !next && onExited()}
    >
      <DialogContent className="flex max-h-[min(88svh,760px)] flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>选择要导入的模型</DialogTitle>
        </DialogHeader>

        <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto">
          {state.kind === "loading" && (
            <div className="text-muted-foreground flex items-center justify-center gap-2 py-10 text-sm">
              <Loader2 className="size-4 animate-spin" />
              正在向上游请求模型列表…
            </div>
          )}

          {state.kind === "error" && (
            <Notice tone="danger" title="拉取失败">
              <p>{state.message}</p>
              <ul className="mt-1.5 list-disc pl-4 text-xs">
                {state.secretMissing ? (
                  <li>
                    {canWriteChannel
                      ? "先点“设置 Key”填写 API Key。"
                      : "请联系 super_admin 设置这个渠道的 API Key。"}
                  </li>
                ) : (
                  <>
                    <li>检查 Base URL 和 API Key 是否正确。</li>
                    <li>
                      该上游可能不提供 <code>/v1/models</code>（视频类上游常见），请手动新增模型。
                    </li>
                  </>
                )}
              </ul>
            </Notice>
          )}

          {state.kind === "ready" && models.length === 0 && (
            <Notice tone="warning">上游返回了空列表，请手动新增模型。</Notice>
          )}

          {state.kind === "ready" && models.length > 0 && (
            <>
              <SearchInput
                aria-label="搜索模型标识"
                placeholder="搜索模型标识，例如 gpt、sd2.5"
                autoComplete="off"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
              />
              <div className="flex flex-wrap items-center gap-2 rounded-lg border px-3 py-2">
                <b className="text-sm tabular-nums">
                  已选择 {stats.selected} / {stats.total} 个模型
                </b>
                <span className="ml-auto flex gap-1.5">
                  <Button
                    size="xs"
                    variant="outline"
                    onClick={() => setSelected(setSelection(models, selected, query, true))}
                  >
                    全选
                  </Button>
                  <Button
                    size="xs"
                    variant="outline"
                    onClick={() => setSelected(setSelection(models, selected, query, false))}
                  >
                    取消全选
                  </Button>
                </span>
              </div>
              <div className="flex max-h-64 min-h-16 flex-wrap content-start gap-2 overflow-y-auto">
                {visible.length === 0 ? (
                  <span className="text-muted-foreground text-xs">没有匹配的模型</span>
                ) : (
                  visible.map((item) => (
                    <PulledChip
                      key={item.id}
                      item={item}
                      checked={selected.has(item.id)}
                      onChange={(checked) =>
                        setSelected((prev) => {
                          const next = new Set(prev);
                          if (checked) next.add(item.id);
                          else next.delete(item.id);
                          return next;
                        })
                      }
                    />
                  ))
                )}
              </div>
              <p className="text-muted-foreground text-xs tabular-nums">
                将导入 {stats.newCount} 个新模型，另有 {stats.existingCount} 个已存在模型已勾选
              </p>
            </>
          )}
        </div>

        <DialogFooter>
          {state.kind === "ready" && models.length > 0 ? (
            <>
              <Button variant="outline" disabled={importing} onClick={onClose}>
                取消
              </Button>
              <Button
                disabled={stats.newCount === 0 || importing}
                onClick={() => void confirmImport()}
              >
                {importing && <Loader2 className="animate-spin" />}
                确认导入
              </Button>
            </>
          ) : (
            <>
              <Button variant="outline" onClick={onClose}>
                关闭
              </Button>
              {state.kind === "error" && (
                <Button variant="outline" onClick={() => void pull()}>
                  <RefreshCw />
                  重试
                </Button>
              )}
              {state.kind !== "loading" && <Button onClick={manualAdd}>手动新增模型</Button>}
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** 一个模型标签：复选框 + 模型 id，已存在的标注“已存在” */
function PulledChip({
  item,
  checked,
  onChange,
}: {
  item: PulledModel;
  checked: boolean;
  onChange: (checked: boolean) => void;
}) {
  const id = `pulled-${item.id}`;
  return (
    <Label
      htmlFor={id}
      className="hover:bg-muted/60 flex cursor-pointer items-center gap-2 rounded-lg border px-2.5 py-1.5 font-normal"
    >
      <Checkbox id={id} checked={checked} onCheckedChange={(next) => onChange(next === true)} />
      <span className="font-mono text-xs break-all">{item.id}</span>
      {item.exists && <Tag>已存在</Tag>}
    </Label>
  );
}

/**
 * 用全局弹窗 store 打开拉取模型弹窗。
 * @param props 渠道、权限与导入后的回调
 */
export const openPullModelsDialog = (props: PullModelsDialogProps) =>
  openDialog(PullModelsDialog, props);
