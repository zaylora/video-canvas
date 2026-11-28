import type { CanvasVersion } from "@/utils/canvas/collaboration-version";

import { useEffect, useRef, useState } from "react";
import { ChevronDown, Crosshair, History, LoaderCircle, RefreshCw } from "lucide-react";

import {
  copyCanvasHistoricalSnapshot,
  getCanvasHistoricalSnapshot,
  getCanvasHistory,
  type CanvasHistoryEntry,
} from "@/api/canvas/collaboration";
import type { CanvasDetailDto } from "@/api/canvas/type";
import { SharedCanvasScene } from "@/components/canvas-editor/shared-scene";
import { Button } from "@/components/ui/button";

const actionLabels = { create: "创建", update: "修改", delete: "删除" };
const targetLabels = { node: "节点", group: "分组", edge: "连线" };
const activityDate = new Intl.DateTimeFormat(undefined, { dateStyle: "long" });
const activityTime = new Intl.DateTimeFormat(undefined, { timeStyle: "medium" });

function describeActivity(targets: CanvasHistoryEntry["targets"]) {
  const first = targets[0];
  if (!first) return "保存了画布";
  if (targets.every(({ action, kind }) => action === first.action && kind === first.kind)) {
    return `${actionLabels[first.action]}了 ${targets.length} ${first.kind === "edge" ? "条" : "个"}${targetLabels[first.kind]}`;
  }
  return `编辑了 ${targets.length} 项画布内容`;
}

function groupActivityByDate(rows: CanvasHistoryEntry[] = []) {
  const days = new Map<string, CanvasHistoryEntry[]>();
  for (const entry of rows) {
    const date = new Date(entry.createdAt);
    const label = Number.isNaN(date.getTime()) ? "日期未知" : activityDate.format(date);
    const entries = days.get(label);
    if (entries) entries.push(entry);
    else days.set(label, [entry]);
  }
  return [...days];
}

export function CanvasVersionHistory({
  canvasId,
  epoch,
  locate,
  openCopy,
}: {
  canvasId: string;
  epoch: CanvasVersion;
  locate: (id: string) => boolean;
  openCopy?: (id: string) => void;
}) {
  const [rows, setRows] = useState<CanvasHistoryEntry[]>();
  const [before, setBefore] = useState<CanvasVersion>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [reload, setReload] = useState(0);
  const [snapshot, setSnapshot] = useState<CanvasDetailDto>();
  const [confirmCopy, setConfirmCopy] = useState(false);
  const [copies, setCopies] = useState<Record<string, string>>({});
  const copyOps = useRef(new Map<CanvasVersion, string>());
  const session = useRef<AbortSignal | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    session.current = controller.signal;
    void getCanvasHistory(canvasId, epoch, undefined, controller.signal).then(
      (page) => {
        if (controller.signal.aborted) return;
        setRows(page.entries);
        setBefore(page.before);
      },
      () => {
        if (controller.signal.aborted) return;
        setRows(undefined);
        setBefore(undefined);
        setError("版本记录暂时无法加载。");
      },
    );
    return () => controller.abort();
  }, [canvasId, epoch, reload]);

  const next = async () => {
    if (!before || busy) return;
    const signal = session.current;
    setBusy(true);
    setError("");
    try {
      const page = await getCanvasHistory(canvasId, epoch, before, signal ?? undefined);
      if (signal?.aborted) return;
      setRows((current) => [...(current ?? []), ...page.entries]);
      setBefore(page.before);
    } catch {
      if (!signal?.aborted) setError("更早的版本暂时无法加载。");
    } finally {
      if (!signal?.aborted) setBusy(false);
    }
  };

  const preview = async (revision: CanvasVersion) => {
    const signal = session.current;
    if (!signal || signal.aborted || busy) return;
    setBusy(true);
    setError("");
    try {
      const result = await getCanvasHistoricalSnapshot(canvasId, epoch, revision, signal);
      if (!signal.aborted) setSnapshot(result);
    } catch {
      if (!signal.aborted) setError("此版本暂时无法预览，可能已超出历史保留范围。");
    } finally {
      if (!signal.aborted) setBusy(false);
    }
  };

  const copy = async (revision: CanvasVersion) => {
    const signal = session.current;
    if (!signal || signal.aborted || busy) return;
    const opId = copyOps.current.get(revision) ?? crypto.randomUUID();
    copyOps.current.set(revision, opId);
    setBusy(true);
    setError("");
    try {
      const result = await copyCanvasHistoricalSnapshot(canvasId, epoch, revision, opId, signal);
      if (signal.aborted) return;
      setCopies((current) => ({ ...current, [revision]: String(result.id) }));
      setConfirmCopy(false);
    } catch {
      if (!signal.aborted) setError("另存失败，请重试。");
    } finally {
      if (!signal.aborted) setBusy(false);
    }
  };

  if (snapshot && openCopy) {
    const copyId = copies[snapshot.version];
    return (
      <div className="grid min-h-0 gap-3 overflow-y-auto overscroll-contain px-6 py-4 [scrollbar-width:thin]">
        <h3 className="text-sm font-medium">版本 {snapshot.version} · 只读预览</h3>
        <div className="border-border relative h-[min(44svh,440px)] overflow-hidden rounded-xl border">
          <SharedCanvasScene graph={snapshot.graph} embedded overlay={() => null} />
        </div>
        {confirmCopy && (
          <p className="text-sm">
            此版本会成为「我的作品」里可继续编辑的个人画布。当前协作画布及成员权限保持不变。
          </p>
        )}
        {error && (
          <p role="alert" className="text-destructive text-sm">
            {error}
          </p>
        )}
        <div className="flex flex-wrap justify-end gap-2">
          <Button
            variant="ghost"
            disabled={busy}
            onClick={() => {
              setSnapshot(undefined);
              setConfirmCopy(false);
              setError("");
            }}
          >
            返回版本列表
          </Button>
          {copyId ? (
            <Button onClick={() => openCopy(copyId)}>打开个人副本</Button>
          ) : confirmCopy ? (
            <Button disabled={busy} onClick={() => void copy(snapshot.version)}>
              确认另存
            </Button>
          ) : (
            <Button onClick={() => setConfirmCopy(true)}>另存到我的作品</Button>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-col">
      <div
        className="min-h-0 space-y-5 overflow-y-auto overscroll-contain px-5 py-4 [scrollbar-width:thin] sm:px-6"
        aria-label="版本活动列表"
      >
        {groupActivityByDate(rows).map(([day, entries]) => (
          <section key={day}>
            <h3 className="text-muted-foreground mb-2 text-xs font-medium">{day}</h3>
            <ul className="ml-1">
              {entries.map((entry) => (
                <VersionActivityRow
                  key={entry.revision}
                  entry={entry}
                  busy={busy}
                  onLocate={(id) => setError(locate(id) ? "" : "该对象当前已不在画布上。")}
                  onPreview={openCopy ? (revision) => void preview(revision) : undefined}
                />
              ))}
            </ul>
          </section>
        ))}
        {!rows?.length && !error && (
          <div
            role="status"
            className="text-muted-foreground grid min-h-32 place-content-center justify-items-center gap-2 text-sm"
          >
            {rows ? (
              <History aria-hidden="true" className="size-5" />
            ) : (
              <LoaderCircle aria-hidden="true" className="size-5 motion-safe:animate-spin" />
            )}
            <p>{rows ? "还没有已确认的编辑。" : "正在加载版本…"}</p>
          </div>
        )}
      </div>
      {error && (
        <p role="alert" className="text-destructive px-6 py-2 text-sm">
          {error}
        </p>
      )}
      <div className="border-chrome-border flex shrink-0 flex-wrap justify-end gap-2 border-t px-5 py-3 sm:px-6">
        <Button
          variant="ghost"
          className="rounded-full"
          disabled={busy}
          onClick={() => {
            setError("");
            setRows(undefined);
            setBefore(undefined);
            setReload((value) => value + 1);
          }}
        >
          <RefreshCw />
          刷新
        </Button>
        {before !== undefined && (
          <Button
            variant="outline"
            className="rounded-full"
            disabled={busy}
            onClick={() => void next()}
          >
            加载更早的版本
          </Button>
        )}
      </div>
    </div>
  );
}

function VersionActivityRow({
  entry,
  onLocate,
  onPreview,
  busy,
}: {
  entry: CanvasHistoryEntry;
  onLocate: (id: string) => void;
  onPreview?: (revision: CanvasVersion) => void;
  busy: boolean;
}) {
  const date = new Date(entry.createdAt);
  const author = entry.authorUsername?.trim() || (entry.authorId ? "账号不可用" : "系统写入");
  return (
    <li className="before:bg-chrome-border relative pl-4 before:absolute before:inset-y-0 before:left-0 before:w-px last:before:bottom-auto last:before:h-5">
      <span
        aria-hidden="true"
        className="bg-muted-foreground absolute top-5 -left-1 size-[7px] rounded-full"
      />
      <details className="group rounded-xl open:bg-chrome-hover">
        <summary className="hover:bg-chrome-hover focus-visible:ring-ring/60 flex min-h-12 cursor-pointer list-none items-start gap-2 rounded-xl px-3 py-2 focus-visible:ring-2 focus-visible:outline-none [&::-webkit-details-marker]:hidden">
          <span className="grid min-w-0 flex-1 gap-0.5">
            <span className="text-sm leading-5 font-medium">{describeActivity(entry.targets)}</span>
            <span className="text-muted-foreground flex flex-wrap items-baseline gap-x-2 text-xs">
              <span className="max-w-full wrap-anywhere">{author}</span>
              <span className="whitespace-nowrap">版本 {entry.revision}</span>
              {!Number.isNaN(date.getTime()) && (
                <time className="whitespace-nowrap tabular-nums" dateTime={entry.createdAt}>
                  {activityTime.format(date)}
                </time>
              )}
            </span>
          </span>
          <ChevronDown
            aria-hidden="true"
            className="text-muted-foreground mt-0.5 size-4 shrink-0 group-open:rotate-180"
          />
        </summary>
        <div className="border-chrome-border mx-3 border-t py-1.5">
          {entry.targets.length > 0 ? (
            <ul className="divide-chrome-border divide-y">
              {entry.targets.map(({ id, kind, action }) => (
                <li
                  key={`${kind}:${id}`}
                  className="flex min-h-10 items-center justify-between gap-2 text-xs"
                >
                  <span className="grid min-w-0 gap-0.5 py-1">
                    <span>
                      {actionLabels[action]}
                      {targetLabels[kind]}
                    </span>
                    <span className="text-muted-foreground wrap-anywhere">{id}</span>
                  </span>
                  {kind !== "edge" && action !== "delete" && (
                    <Button
                      size="sm"
                      variant="ghost"
                      className="rounded-full"
                      aria-label={`定位${targetLabels[kind]} ${id}`}
                      onClick={() => onLocate(id)}
                    >
                      <Crosshair />
                      定位
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-muted-foreground py-2 text-xs">无对象变更</p>
          )}
          {onPreview && (
            <div className="flex justify-end py-1">
              <Button
                size="sm"
                variant="outline"
                className="rounded-full"
                disabled={busy}
                onClick={() => onPreview(entry.revision)}
              >
                预览版本
              </Button>
            </div>
          )}
        </div>
      </details>
    </li>
  );
}
