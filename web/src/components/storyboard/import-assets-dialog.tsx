import { useMemo, useState } from "react";
import { useStore } from "@xyflow/react";
import { Check, FolderInput, Image as ImageIcon, Video } from "lucide-react";

import { VideoPoster } from "@/components/canvas/media-preview";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import type { CanvasNodeData, NodeStoryboard } from "@/types";
import { seqText } from "@/utils/storyboard/compose";
import {
  canvasAssetsOf,
  defaultSelection,
  importKindName,
  importTargets,
  planImport,
  type AssetNode,
  type CanvasAsset,
  type ImportKind,
  type ImportLink,
} from "@/utils/storyboard/import-assets";

/** 打开导入面板时的意图：导入哪种素材；给了 shotId 就是只挂这一镜 */
export type ImportRequest = { kind: ImportKind; shotId?: string };

/** 确认导入时要写回分镜表的东西 */
export type ImportResult = {
  kind: ImportKind;
  links: ImportLink[];
  /** 空镜头不够、要多追加的行 */
  appended: { nodeId: string; label: string }[];
};

/** 两种素材各自的图标 */
const KIND_ICON = { video: Video, image: ImageIcon } as const;

/** 列表里一枚缩略图：视频取封面（不挂 video，零视频请求），图片直接显示 */
function AssetThumb({ asset, kind }: { asset: CanvasAsset; kind: ImportKind }) {
  return (
    // 描一圈边：没有封面时底色和面板几乎一样，不描边就像空着一格
    <span className="bg-muted text-muted-foreground/50 ring-border/70 grid h-9 w-16 shrink-0 place-items-center overflow-hidden rounded-md ring-1">
      {!asset.src ? (
        kind === "video" ? (
          <Video className="size-4" />
        ) : (
          <ImageIcon className="size-4" />
        )
      ) : kind === "video" ? (
        <VideoPoster src={asset.src} iconClassName="size-4" />
      ) : (
        <img src={asset.src} alt="" draggable={false} className="size-full object-cover" />
      )}
    </span>
  );
}

/** 一枚标记：已经挂在哪几镜 */
function UsedTag({ shots }: { shots: number[] }) {
  return (
    <span className="bg-muted text-muted-foreground shrink-0 rounded-md px-1.5 py-0.5 text-[11px]">
      已挂第 {seqText(shots.map((n) => n - 1))} 镜
    </span>
  );
}

/** 列表里的一行：左边缩略图、标题和状态，右边是勾选框或「挂到这一镜」的提示 */
function AssetRow({
  asset,
  kind,
  picked,
  current,
  onToggle,
  onPick,
}: {
  asset: CanvasAsset;
  kind: ImportKind;
  /** 批量模式：勾中了没有 */
  picked?: boolean;
  /** 单行模式：这一镜现在挂的就是它 */
  current?: boolean;
  onToggle?: () => void;
  /** 单行模式：点了直接挂到这一镜 */
  onPick?: () => void;
}) {
  const body = (
    <>
      <span className="text-muted-foreground w-5 shrink-0 text-center text-xs tabular-nums">
        {asset.seq}
      </span>
      <AssetThumb asset={asset} kind={kind} />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm">{asset.label}</span>
        <span className="text-muted-foreground block text-[11px]">
          {asset.ready
            ? kind === "video"
              ? "已出片"
              : "有图"
            : kind === "video"
              ? "还没出片"
              : "还没有图"}
        </span>
      </span>
      {asset.usedBy.length > 0 && <UsedTag shots={asset.usedBy} />}
    </>
  );

  if (onPick) {
    return (
      <button
        type="button"
        onClick={onPick}
        className="hover:bg-chrome-hover focus-visible:bg-chrome-hover flex w-full items-center gap-2.5 rounded-lg px-2 py-1.5 text-left outline-none"
      >
        {body}
        {current ? (
          <Check className="text-status-success size-4 shrink-0" />
        ) : (
          <span className="text-muted-foreground shrink-0 text-xs">挂到这一镜</span>
        )}
      </button>
    );
  }
  return (
    <div
      className={cn(
        "hover:bg-chrome-hover flex items-center gap-2.5 rounded-lg px-2 py-1.5",
        picked && "bg-chrome-hover",
      )}
    >
      <Checkbox
        checked={picked}
        onCheckedChange={() => onToggle?.()}
        aria-label={`选择 ${asset.label}`}
      />
      <button
        type="button"
        onClick={onToggle}
        className="flex min-w-0 flex-1 items-center gap-2.5 text-left outline-none"
      >
        {body}
      </button>
    </div>
  );
}

/**
 * 导入画布素材：把画布上已经存在的视频 / 图片节点挂到分镜表的镜头行上。
 *
 * 分镜表是后来加的、或者镜头不是从这张表生成的时，行上只有「生成」一条路，已有素材挂不进去。
 * 这里两个模式，同一份列表：
 * - 单行（request.shotId 有值）：点一项就挂到这一镜，用来换掉某一镜的素材；
 * - 批量：勾几段，按画布顺序补进还没有素材的镜头；空镜头不够时可以把多出来的追加成新行。
 *
 * 列表顺序只认画布位置（上→下、同高左→右），和成片、和小镜看到的是同一套规则。
 */
export function ImportAssetsDialog({
  open,
  onOpenChange,
  board,
  request,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  board: NodeStoryboard;
  request: ImportRequest;
  onConfirm: (result: ImportResult) => void;
}) {
  const [kind, setKind] = useState<ImportKind>(request.kind);
  const [seen, setSeen] = useState(request);
  /** null 表示还没手动改过：按默认规则（只勾没被用过的）算 */
  const [picked, setPicked] = useState<string[] | null>(null);
  const [append, setAppend] = useState(false);
  const lookup = useStore((state) => state.nodeLookup);

  // 换了一次导入意图（另一种素材、另一镜）：临时勾选全部作废，按新意图从头来。
  // 表里一行都没有素材位（新加的空表）时直接勾上「追加为镜头」，否则导进来无处可放。
  if (seen !== request) {
    setSeen(request);
    setKind(request.kind);
    setPicked(null);
    setAppend(request.shotId === undefined && importTargets(board, request.kind).length === 0);
  }

  const single = request.shotId !== undefined;
  const targetIndex = single ? board.shots.findIndex((s) => s.id === request.shotId) : -1;
  const currentLink =
    targetIndex >= 0
      ? kind === "video"
        ? board.shots[targetIndex].videoNodeId
        : board.shots[targetIndex].imageNodeId
      : undefined;

  const assets = useMemo(() => {
    const nodes: AssetNode[] = [];
    lookup.forEach((node) => {
      nodes.push({
        id: node.id,
        position: node.position,
        data: node.data as CanvasNodeData,
      });
    });
    return canvasAssetsOf(nodes, kind, board);
  }, [lookup, kind, board]);

  const effective = picked ?? defaultSelection(assets);
  const pickedAssets = useMemo(
    () => assets.filter((asset) => effective.includes(asset.id)),
    [assets, effective],
  );
  const plan = useMemo(
    () => planImport(board, pickedAssets, kind, { append }),
    [board, pickedAssets, kind, append],
  );

  const targets = importTargets(board, kind);
  const extra = Math.max(0, pickedAssets.length - targets.length);
  const linkNumbers = plan.links.map(
    (link) => board.shots.findIndex((s) => s.id === link.shotId) + 1,
  );

  const KindIcon = KIND_ICON[kind];
  const title = single
    ? `导入${importKindName(kind)}到第 ${String(targetIndex + 1).padStart(2, "0")} 镜`
    : "导入画布素材";

  /** 关掉：临时选择留到下次打开时按新意图重置，这里只关面板 */
  const close = () => onOpenChange(false);

  const toggle = (id: string) => {
    const next = effective.includes(id)
      ? effective.filter((item) => item !== id)
      : [...effective, id];
    // 顺序始终按画布顺序，不按点的先后：列表怎么排就怎么挂
    setPicked(assets.filter((asset) => next.includes(asset.id)).map((asset) => asset.id));
  };

  const confirm = () => {
    onConfirm({ kind, links: plan.links, appended: plan.appended });
    close();
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) close();
      }}
    >
      <DialogContent className="flex h-[min(78svh,620px)] w-[min(680px,94vw)] max-w-none flex-col gap-0 overflow-hidden p-0 sm:max-w-none">
        <div className="flex shrink-0 flex-col gap-1 border-b border-border/60 py-3 pr-12 pl-5">
          <DialogTitle className="flex items-center gap-2 text-sm font-semibold">
            <FolderInput className="text-muted-foreground size-4" />
            {title}
          </DialogTitle>
          <DialogDescription className="text-muted-foreground text-xs">
            画布上已有 {assets.length} 个{importKindName(kind)}节点
            {single
              ? "，点一个挂到这一镜。素材不会被复制，只是记下用哪个节点。"
              : "，按画布顺序（上→下）挂到还没有素材的镜头。"}
          </DialogDescription>
        </div>

        {/* 单行也要能切：某一镜想换的可能是图、也可能是视频，不该由「点的是哪一格」决定 */}
        <div className="flex shrink-0 gap-1 px-4 pt-3">
          {(Object.keys(KIND_ICON) as ImportKind[]).map((item) => {
            const Icon = KIND_ICON[item];
            return (
              <button
                key={item}
                type="button"
                aria-pressed={kind === item}
                onClick={() => {
                  setKind(item);
                  setPicked(null);
                  setAppend(!single && importTargets(board, item).length === 0);
                }}
                className={cn(
                  "focus-visible:ring-node-ring flex h-8 items-center gap-1.5 rounded-full px-3 text-xs outline-none transition-colors focus-visible:ring-2",
                  kind === item
                    ? "bg-muted text-foreground"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                <Icon className="size-3.5" />
                {importKindName(item)}
              </button>
            );
          })}
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto px-3 py-2">
          {assets.length === 0 ? (
            <div className="text-muted-foreground grid h-full place-items-center px-6 text-center text-sm">
              画布上还没有{importKindName(kind)}节点，先在画布上生成或上传一个。
            </div>
          ) : (
            <div className="flex flex-col gap-0.5">
              {assets.map((asset) =>
                single ? (
                  <AssetRow
                    key={asset.id}
                    asset={asset}
                    kind={kind}
                    current={asset.id === currentLink}
                    onPick={() => {
                      onConfirm({
                        kind,
                        links: [{ shotId: request.shotId!, nodeId: asset.id }],
                        appended: [],
                      });
                      close();
                    }}
                  />
                ) : (
                  <AssetRow
                    key={asset.id}
                    asset={asset}
                    kind={kind}
                    picked={effective.includes(asset.id)}
                    onToggle={() => toggle(asset.id)}
                  />
                ),
              )}
            </div>
          )}
        </div>

        {/* 单行：点一项就挂上，这里只留一条说明和一个退出口，免得面板下半截空着 */}
        {single && (
          <div className="flex shrink-0 items-center gap-2 border-t border-border/60 px-4 py-3">
            <p className="text-muted-foreground min-w-0 flex-1 text-xs">
              {currentLink
                ? "当前挂着打勾的那一项，点别的就能换掉；关掉面板什么都不会改。"
                : "点一项挂到这一镜；关掉面板什么都不会改。"}
            </p>
            <Button variant="ghost" size="sm" onClick={close}>
              取消
            </Button>
          </div>
        )}

        {!single && (
          <div className="flex shrink-0 flex-col gap-2 border-t border-border/60 px-4 py-3">
            {extra > 0 && (
              <label className="text-muted-foreground flex items-center gap-2 text-xs">
                <Checkbox checked={append} onCheckedChange={(v) => setAppend(v === true)} />
                多出的 {extra} 段追加为镜头（画面描述先用节点标题）
              </label>
            )}
            <div className="flex items-center gap-2">
              <p className="text-muted-foreground min-w-0 flex-1 text-xs">
                {pickedAssets.length === 0 ? (
                  "勾选要导入的节点"
                ) : (
                  <>
                    {plan.links.length > 0 && (
                      <>挂到第 {seqText(linkNumbers.map((n) => n - 1))} 镜</>
                    )}
                    {plan.appended.length > 0 && (
                      <>
                        {plan.links.length > 0 ? "；" : ""}
                        追加 {plan.appended.length} 行
                      </>
                    )}
                    {plan.leftover > 0 && (
                      <span className="text-status-running">
                        {plan.links.length + plan.appended.length > 0 ? "；" : ""}
                        还有 {plan.leftover} 段没有空镜头可挂
                      </span>
                    )}
                  </>
                )}
              </p>
              <Button variant="ghost" size="sm" onClick={close}>
                取消
              </Button>
              <Button size="sm" disabled={pickedAssets.length === 0} onClick={confirm}>
                <KindIcon />
                {pickedAssets.length > 0
                  ? `导入 ${pickedAssets.length} ${kind === "video" ? "段" : "张"}`
                  : "导入"}
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
