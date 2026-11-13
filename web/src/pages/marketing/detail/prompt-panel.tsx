import { motion } from "motion/react";
import { useCallback, useEffect, useState } from "react";
import { toast } from "sonner";
import { Copy, Languages, Loader2, RefreshCw, TriangleAlert } from "lucide-react";

import { adaptMarketingScript, editMarketingAdaptation, getMarketingPrompt } from "@/api/marketing";
import type { Round, Script, ScriptPrompt } from "@/api/marketing/type";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";
import { useCreditsStore } from "@/store/credits";
import { reloadMarketingProject } from "@/utils/marketing/events";

type View = "source" | "adapted";

type LoadState =
  | { status: "loading" }
  | { status: "error" }
  | { status: "ready"; data: ScriptPrompt };

/** 复制到剪贴板，提示成功或失败 */
async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text);
    toast.success("已复制");
  } catch {
    toast.error("复制失败，请手动选中复制");
  }
}

/**
 * 提示词页：Seedance / 通用模型直接显示出片用的提示词；
 * H3 多一个转写标签：原稿（Seedance 写法）由文本模型转写成 H3 写法，可以对照、手改、重新转写，
 * 出片时没有转写稿或原稿变了会自动转写。
 */
export function PromptPanel({
  projectId,
  script,
  round,
}: {
  projectId: string;
  script: Script;
  round: Round;
}) {
  const [state, setState] = useState<LoadState>({ status: "loading" });
  const videoModel = round.settings.videoModel;

  useEffect(() => {
    let alive = true;
    setState({ status: "loading" });
    getMarketingPrompt(script.id, videoModel)
      .then((data) => alive && setState({ status: "ready", data }))
      .catch(() => alive && setState({ status: "error" }));
    return () => {
      alive = false;
    };
    // 出片后（自动转写）脚本状态会变，顺带刷新转写稿
  }, [script.id, script.revision, script.status, videoModel]);

  if (state.status !== "ready") {
    return (
      <div className="p-4">
        <pre className="bg-chrome-hover min-h-32 rounded-[18px] p-3 font-mono text-[11.5px]">
          {state.status === "loading" ? (
            <span className="text-muted-foreground inline-flex items-center gap-2">
              <Loader2 className="size-3.5 animate-spin" />
              拼装中
            </span>
          ) : (
            <span className="text-destructive">提示词加载失败</span>
          )}
        </pre>
      </div>
    );
  }
  if (state.data.format !== "h3") {
    return (
      <div className="grid gap-2 p-4">
        <div className="flex items-center justify-end">
          <Button variant="ghost" size="sm" onClick={() => void copyText(state.data.prompt)}>
            <Copy />
            复制
          </Button>
        </div>
        <PromptText text={state.data.prompt} />
      </div>
    );
  }
  return (
    <AdaptPanel
      key={script.id}
      projectId={projectId}
      script={script}
      videoModel={videoModel}
      data={state.data}
      onChange={(data) => setState({ status: "ready", data })}
    />
  );
}

/** 只读的提示词全文 */
function PromptText({ text }: { text: string }) {
  return (
    <pre className="bg-chrome-hover min-h-32 overflow-auto rounded-[18px] p-3 font-mono text-[11.5px] leading-relaxed whitespace-pre-wrap">
      {text}
    </pre>
  );
}

/** H3：原稿 / 转写两个标签、转写按钮、可编辑的转写稿 */
function AdaptPanel({
  projectId,
  script,
  videoModel,
  data,
  onChange,
}: {
  projectId: string;
  script: Script;
  videoModel: string;
  data: ScriptPrompt;
  onChange: (data: ScriptPrompt) => void;
}) {
  const adaptation = data.adaptation;
  const [view, setView] = useState<View>(adaptation ? "adapted" : "source");
  const [busy, setBusy] = useState(false);
  const locked = script.status === "rewriting" || script.status === "rendering";

  const run = useCallback(async () => {
    setView("adapted");
    setBusy(true);
    try {
      onChange(await adaptMarketingScript(script.id, videoModel));
      void useCreditsStore.getState().refresh();
      // 出片报价随转写稿是否有效而变
      void reloadMarketingProject(projectId);
    } catch {
      // 请求层已提示（积分不足、转写失败）
    } finally {
      setBusy(false);
    }
  }, [onChange, projectId, script.id, videoModel]);

  const credits = data.adaptCredits > 0 ? `✦${data.adaptCredits}` : "";
  return (
    <div className="grid gap-2.5 p-4">
      <div className="flex flex-wrap items-center gap-2">
        <div
          role="tablist"
          aria-label="提示词格式"
          className="bg-chrome ring-chrome-border relative flex rounded-full p-0.5 ring-1"
        >
          {(
            [
              ["source", "原稿 · Seedance"],
              ["adapted", "转写 · H3"],
            ] as const
          ).map(([value, text]) => (
            <button
              key={value}
              type="button"
              role="tab"
              aria-selected={view === value}
              onClick={() => setView(value)}
              className={cn(
                "focus-visible:ring-ring/50 relative flex h-7 items-center gap-1.5 rounded-full px-3 text-xs font-medium outline-none focus-visible:ring-2",
                view === value ? "text-foreground" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {view === value && (
                <motion.span
                  layoutId="marketing-prompt-format"
                  transition={SPRING}
                  className="bg-chrome-hover ring-chrome-border absolute inset-0 rounded-full ring-1"
                />
              )}
              <span className="relative">{text}</span>
              {value === "adapted" && <AdaptDot busy={busy} adaptation={adaptation} />}
            </button>
          ))}
        </div>
        <span className="flex-1" />
        <Button
          variant="ghost"
          size="sm"
          title="复制"
          aria-label="复制"
          onClick={() =>
            void copyText(view === "source" ? data.prompt : (adaptation?.prompt ?? ""))
          }
        >
          <Copy />
        </Button>
        {adaptation && (
          <Button
            variant="ghost"
            size="sm"
            className="rounded-full"
            disabled={busy || locked}
            onClick={() => void run()}
          >
            {busy ? <Loader2 className="animate-spin" /> : <RefreshCw />}
            重新转写
            {credits && <span className="tabular-nums opacity-70">{credits}</span>}
          </Button>
        )}
      </div>

      {view === "source" ? (
        <PromptText text={data.prompt} />
      ) : adaptation ? (
        <div className="grid gap-2">
          {adaptation.stale && !busy && (
            <div className="text-status-warning bg-status-warning/10 flex items-center gap-2 rounded-xl px-3 py-2 text-xs [&_svg]:size-3.5">
              <TriangleAlert />
              原稿已修改
            </div>
          )}
          <AdaptEditor
            key={adaptation.updatedAt}
            scriptId={script.id}
            prompt={adaptation.prompt}
            disabled={busy}
            onSaved={onChange}
          />
        </div>
      ) : (
        <div className="bg-chrome-hover text-muted-foreground grid min-h-36 place-items-center content-center gap-3 rounded-[18px] px-4 py-8 text-sm">
          {busy ? <Loader2 className="size-6 animate-spin" /> : <Languages className="size-6" />}
          <span>{busy ? "正在转写为 H3" : "还没有 H3 转写稿"}</span>
          {!busy && (
            <Button
              size="sm"
              variant="outline"
              className="rounded-full"
              disabled={locked}
              onClick={() => void run()}
            >
              转写为 H3
              {credits && <span className="tabular-nums opacity-70">{credits}</span>}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

/** 可编辑的转写稿：改了才出现还原 / 保存；服务端换了新稿（转写完、出片时重新转写）按 key 重挂 */
function AdaptEditor({
  scriptId,
  prompt,
  disabled,
  onSaved,
}: {
  scriptId: string;
  prompt: string;
  disabled: boolean;
  onSaved: (data: ScriptPrompt) => void;
}) {
  const [draft, setDraft] = useState(prompt);
  const [saving, setSaving] = useState(false);
  const save = async () => {
    setSaving(true);
    try {
      onSaved(await editMarketingAdaptation(scriptId, draft));
      toast.success("转写稿已保存");
    } catch {
      // 请求层已提示
    } finally {
      setSaving(false);
    }
  };
  return (
    <>
      <Textarea
        value={draft}
        disabled={disabled || saving}
        onChange={(event) => setDraft(event.target.value)}
        className="bg-chrome-hover min-h-64 rounded-[18px] p-3 font-mono text-[11.5px] leading-relaxed"
      />
      {draft !== prompt && (
        <div className="flex justify-end gap-2">
          <Button variant="ghost" size="sm" disabled={saving} onClick={() => setDraft(prompt)}>
            还原
          </Button>
          <Button size="sm" disabled={saving || !draft.trim()} onClick={() => void save()}>
            {saving && <Loader2 className="animate-spin" />}
            保存
          </Button>
        </div>
      )}
    </>
  );
}

/** 转写标签上的状态：转圈 = 转写中，绿点 = 最新，黄点 + 已过期 */
function AdaptDot({ busy, adaptation }: { busy: boolean; adaptation: ScriptPrompt["adaptation"] }) {
  if (busy) return <Loader2 className="relative size-3 animate-spin" />;
  if (!adaptation) return null;
  if (adaptation.stale) {
    return (
      <span className="text-status-warning relative flex items-center gap-1">
        <span className="bg-status-warning size-1.5 rounded-full" />
        已过期
      </span>
    );
  }
  return <span aria-label="最新" className="bg-status-success relative size-1.5 rounded-full" />;
}
