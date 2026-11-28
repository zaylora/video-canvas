import type { CanvasVersion } from "@/utils/canvas/collaboration-version";

import { useCallback, useEffect, useRef, useState } from "react";

import {
  addCanvasComment,
  getCanvasComments,
  type CanvasCommentRequest,
} from "@/api/canvas/collaboration";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/utils/requests/request";

const accessDenied = (failure: unknown) =>
  failure instanceof ApiError &&
  ([401, 403, 404].includes(failure.status ?? 0) || failure.code === 30018);

type CommentPage = Awaited<ReturnType<typeof getCanvasComments>> & { keepHistory?: true };

function mergeLatestComments(current: CommentPage | undefined, latest: CommentPage): CommentPage {
  if (!current?.keepHistory || !latest.before) return latest;
  const anchor = current.comments.findIndex((note) => note.id === latest.before);
  if (anchor === -1) return latest;
  return {
    ...latest,
    comments: [...latest.comments, ...current.comments.slice(anchor + 1)],
    usernames: { ...current.usernames, ...latest.usernames },
    before: current.before,
    keepHistory: true,
  };
}

export function CanvasCommentThread({
  canvasId,
  nodeId,
  epoch,
}: {
  canvasId: string;
  nodeId: string;
  epoch: CanvasVersion;
}) {
  const [page, setPage] = useState<CommentPage>();
  const [body, setBody] = useState("");
  const [mentions, setMentions] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [error, setError] = useState("");
  const [access, setAccess] = useState(true);
  const [reload, setReload] = useState(0);
  const request = useRef<CanvasCommentRequest | null>(null);
  const threadLife = useRef<AbortController | null>(null);
  const lastCommit = useRef(0);
  const clearAccess = useCallback(() => {
    threadLife.current?.abort();
    setPage(undefined);
    setBody("");
    setMentions([]);
    request.current = null;
    setAccess(false);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    threadLife.current = controller;
    let loading = false;
    const load = () => {
      if (loading || controller.signal.aborted) return;
      loading = true;
      const commit = lastCommit.current;
      void getCanvasComments(canvasId, nodeId, epoch, undefined, controller.signal)
        .then((next) => {
          if (!controller.signal.aborted && commit === lastCommit.current)
            setPage((current) => mergeLatestComments(current, next));
        })
        .catch((failure: unknown) => {
          if (controller.signal.aborted) return;
          if (accessDenied(failure)) clearAccess();
          else setError("评论暂时无法加载。");
        })
        .finally(() => {
          loading = false;
        });
    };
    load();
    const loadWhenVisible = () => {
      if (document.visibilityState === "visible") load();
    };
    const timer = setInterval(loadWhenVisible, 5000);
    document.addEventListener("visibilitychange", loadWhenVisible);
    return () => {
      controller.abort();
      clearInterval(timer);
      document.removeEventListener("visibilitychange", loadWhenVisible);
    };
  }, [canvasId, nodeId, epoch, reload, clearAccess]);

  const send = async () => {
    if (busy || !body.trim() || !access) return;
    const signal = threadLife.current?.signal;
    if (!signal || signal.aborted) return;
    request.current ??= { epoch, nodeId, opId: crypto.randomUUID(), body: body.trim(), mentions };
    setBusy(true);
    setError("");
    try {
      const result = await addCanvasComment(canvasId, request.current, signal);
      if (signal.aborted) return;
      lastCommit.current++;
      setPage((current) =>
        current
          ? {
              ...current,
              comments: [result, ...current.comments.filter((note) => note.id !== result.id)],
            }
          : { comments: [result], members: [] },
      );
      request.current = null;
      setBody("");
      setMentions([]);
    } catch (failure) {
      if (signal.aborted) return;
      if (accessDenied(failure)) clearAccess();
      else setError(failure instanceof Error ? failure.message : "评论未发送。");
    } finally {
      if (!signal.aborted) setBusy(false);
    }
  };

  const loadOlder = async () => {
    const cursor = page?.before;
    const signal = threadLife.current?.signal;
    if (!cursor || loadingOlder || !signal || signal.aborted) return;
    setLoadingOlder(true);
    setPage((current) =>
      current?.before === cursor ? { ...current, keepHistory: true } : current,
    );
    try {
      const result = await getCanvasComments(canvasId, nodeId, epoch, cursor, signal);
      if (signal.aborted) return;
      setPage((current) => {
        if (current?.before !== cursor) return current;
        const shown = new Set(current.comments.map((note) => note.id));
        return {
          ...current,
          comments: [...current.comments, ...result.comments.filter((note) => !shown.has(note.id))],
          usernames: { ...result.usernames, ...current.usernames },
          before: result.before,
        };
      });
    } catch (failure) {
      if (signal.aborted) return;
      if (accessDenied(failure)) clearAccess();
      else setError("更早的评论暂时无法加载。");
    } finally {
      if (!signal.aborted) setLoadingOlder(false);
    }
  };

  if (!access) return <p role="status">此节点的讨论已不可访问。</p>;
  const notes = page?.comments ?? [];
  const mentionable = (page?.members ?? []).flatMap((id) => {
    const username = page?.usernames?.[id];
    return username ? [{ id, username }] : [];
  });
  return (
    <div className="grid gap-3">
      <div className="max-h-64 space-y-2 overflow-y-auto" aria-label="节点评论列表">
        {notes.map((note) => {
          const mentionNames = (note.mentions ?? [])
            .map((id) => page?.usernames?.[id])
            .filter(Boolean);
          return (
            <article
              key={note.id}
              className="border-border/70 grid gap-1 rounded-lg border p-3 text-sm"
            >
              <span className="text-muted-foreground text-xs">
                {page?.usernames?.[note.authorId] || note.authorUsername || "账号不可用"}
              </span>
              <p className="break-words whitespace-pre-wrap">{note.body}</p>
              {!!mentionNames.length && (
                <p className="text-muted-foreground text-xs">
                  {mentionNames.map((username) => `@${username}`).join(" · ")}
                </p>
              )}
            </article>
          );
        })}
        {!notes.length && (
          <p className="text-muted-foreground text-sm">{page ? "还没有评论。" : "正在加载评论…"}</p>
        )}
      </div>
      {page?.before && (
        <Button size="sm" variant="ghost" disabled={loadingOlder} onClick={() => void loadOlder()}>
          加载更早的评论
        </Button>
      )}
      <form
        className="grid gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          void send();
        }}
      >
        <Textarea
          aria-label="评论内容"
          placeholder="写下要交接的事项"
          value={body}
          maxLength={2000}
          disabled={busy}
          onChange={(event) => {
            request.current = null;
            setBody(event.target.value);
          }}
        />
        <div className="flex flex-wrap gap-1" aria-label="可提及成员">
          {mentionable.map(({ id, username }) => (
            <Button
              key={id}
              type="button"
              size="sm"
              variant={mentions.includes(id) ? "secondary" : "ghost"}
              aria-label={`提及${username}`}
              aria-pressed={mentions.includes(id)}
              disabled={busy || (mentions.length >= 10 && !mentions.includes(id))}
              onClick={() => {
                request.current = null;
                setMentions((current) =>
                  current.includes(id)
                    ? current.filter((member) => member !== id)
                    : [...current, id],
                );
              }}
            >
              @{username}
            </Button>
          ))}
        </div>
        {error && (
          <p role="alert" className="text-destructive text-sm">
            {error}
          </p>
        )}
        <div className="flex justify-end gap-2">
          {error && !page && (
            <Button
              type="button"
              variant="ghost"
              onClick={() => {
                setError("");
                setReload((value) => value + 1);
              }}
            >
              重试加载
            </Button>
          )}
          <Button type="submit" disabled={busy || !body.trim()}>
            {busy ? "发送中…" : "发送评论"}
          </Button>
        </div>
      </form>
    </div>
  );
}
