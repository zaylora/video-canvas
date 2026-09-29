import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";
import {
  ArrowLeft,
  Braces,
  CheckCheck,
  FlaskConical,
  History,
  Loader2,
  Play,
  Plus,
  Rocket,
  Save,
  ShieldAlert,
  Undo2,
  Wand2,
} from "lucide-react";

import {
  createDraft,
  dryRunModel,
  getConfigDetail,
  getTestRun,
  importRunningHub,
  listConfigs,
  listRevisions,
  publishConfig,
  rollbackConfig,
  setModelEnabled,
  testRunModel,
  updateDraft,
  validateConfig,
} from "@/api/admin-ai";
import type {
  ConfigDetail,
  ConfigListItem,
  ConfigRevision,
  ConfigTarget,
} from "@/api/admin-ai/type";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { ApiError } from "@/utils/requests/request";
import { formatJsonText, parseJsonText, readConfigKey, toJsonText } from "@/utils/admin/json";
import { isTerminalStatus } from "@/utils/tasks/status";

import { ResultPanel, type ResultEntry } from "./result-panel";
import { SecretsPanel } from "./secrets-panel";
import { MODEL_TEMPLATE, PROVIDER_TEMPLATE } from "./templates";

type Selection = { target: ConfigTarget; key: string | null };
type ListState = "loading" | "ready" | "forbidden" | "error";

const TARGET_LABEL: Record<ConfigTarget, string> = { provider: "平台", model: "模型" };
const MAX_RESULTS = 30;
const TEST_POLL_INTERVAL = 3000;
const TEST_POLL_MAX = 15 * 60 * 1000;

const isForbidden = (error: unknown) =>
  error instanceof ApiError && (error.status === 403 || error.code === 10004);

/**
 * 管理员 AI 配置页：左侧平台 / 模型列表，中间 JSON 编辑器，右侧结果面板。
 * 编辑器是 textarea + 格式化 / 语法校验；服务端校验（validate）会带 JSON 路径。
 * 非管理员访问后端会返回 403，这里展示无权限提示。
 */
export default function AdminAi() {
  const [tab, setTab] = useState<"config" | "secrets">("config");
  const [target, setTarget] = useState<ConfigTarget>("model");
  const [lists, setLists] = useState<Record<ConfigTarget, ConfigListItem[]>>({
    provider: [],
    model: [],
  });
  const [listState, setListState] = useState<ListState>("loading");
  const [selection, setSelection] = useState<Selection | null>(null);
  const [detail, setDetail] = useState<ConfigDetail | null>(null);
  const [text, setText] = useState("");
  const [savedText, setSavedText] = useState("");
  const [sample, setSample] = useState("{}");
  const [useProviderDraft, setUseProviderDraft] = useState(false);
  const [results, setResults] = useState<ResultEntry[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [webappId, setWebappId] = useState("");
  const [revisions, setRevisions] = useState<ConfigRevision[] | null>(null);
  const aliveRef = useRef(true);

  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
    };
  }, []);

  const parsed = useMemo(() => parseJsonText(text), [text]);
  const dirty = text !== savedText;
  const key = selection?.key ?? null;
  const isNew = !!selection && selection.key === null;

  const pushResult = useCallback((entry: Omit<ResultEntry, "id" | "time">) => {
    const id = crypto.randomUUID();
    setResults((prev) => [{ ...entry, id, time: Date.now() }, ...prev].slice(0, MAX_RESULTS));
    return id;
  }, []);

  const patchResult = useCallback((id: string, patch: Partial<ResultEntry>) => {
    setResults((prev) => prev.map((entry) => (entry.id === id ? { ...entry, ...patch } : entry)));
  }, []);

  const loadLists = useCallback(async () => {
    try {
      const [provider, model] = await Promise.all([listConfigs("provider"), listConfigs("model")]);
      if (!aliveRef.current) return;
      setLists({ provider, model });
      setListState("ready");
    } catch (error) {
      if (!aliveRef.current) return;
      setListState(isForbidden(error) ? "forbidden" : "error");
    }
  }, []);

  useEffect(() => {
    void loadLists();
  }, [loadLists]);

  const openDetail = useCallback(
    async (nextTarget: ConfigTarget, nextKey: string) => {
      setSelection({ target: nextTarget, key: nextKey });
      setRevisions(null);
      setBusy("load");
      try {
        const data = await getConfigDetail(nextTarget, nextKey);
        if (!aliveRef.current) return;
        setDetail(data);
        const body = data.draft?.body_json ?? data.published?.body_json ?? {};
        const content = toJsonText(body);
        setText(content);
        setSavedText(content);
      } finally {
        if (aliveRef.current) setBusy(null);
      }
    },
    [pushResult],
  );

  const startNew = (nextTarget: ConfigTarget, body: unknown = nextTarget === "provider" ? PROVIDER_TEMPLATE : MODEL_TEMPLATE) => {
    setTarget(nextTarget);
    setSelection({ target: nextTarget, key: null });
    setDetail(null);
    setRevisions(null);
    setText(toJsonText(body));
    setSavedText("");
  };

  const format = () => {
    const result = formatJsonText(text);
    if (result.ok) setText(result.text);
    else pushResult({ title: "无法格式化", tone: "error", text: result.message });
  };

  /** 保存草稿；成功返回 true。有校验问题也照常保存（后端允许存半成品），只是发布会被拦 */
  const save = async (silentOk = false): Promise<boolean> => {
    if (!selection) return false;
    if (!parsed.ok) {
      pushResult({ title: "JSON 格式错误，无法保存", tone: "error", text: parsed.message });
      return false;
    }
    const bodyKey = readConfigKey(parsed.value);
    if (!bodyKey) {
      pushResult({ title: "正文里缺少 key", tone: "error", text: "key 是配置的唯一标识，必须填写。" });
      return false;
    }
    setBusy("save");
    try {
      const result = isNew
        ? await createDraft(selection.target, parsed.value)
        : await updateDraft(selection.target, selection.key as string, parsed.value);
      const content = toJsonText(parsed.value);
      setSavedText(content);
      if (isNew) {
        setSelection({ target: selection.target, key: bodyKey });
        void openDetail(selection.target, bodyKey);
      }
      if (!silentOk || result.issues.length > 0) {
        pushResult({
          title: `草稿已保存（第 ${result.revision.revision_no} 版）`,
          tone: result.issues.length > 0 ? "info" : "success",
          text: result.issues.length > 0 ? `有 ${result.issues.length} 个问题，修复后才能发布。` : undefined,
          issues: result.issues,
        });
      }
      void loadLists();
      return true;
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  /** dry-run / 试跑 / 发布都基于服务端已保存的草稿，有未保存改动先存 */
  const ensureSaved = async () => (dirty || isNew ? save(true) : true);

  const validate = async () => {
    if (!selection) return;
    if (!parsed.ok) {
      pushResult({ title: "JSON 格式错误", tone: "error", text: parsed.message });
      return;
    }
    setBusy("validate");
    try {
      const result = await validateConfig(selection.target, key ?? readConfigKey(parsed.value), parsed.value);
      pushResult({
        title: result.valid ? "校验通过" : `校验发现 ${result.issues.length} 个问题`,
        tone: result.valid ? "success" : "error",
        issues: result.issues,
      });
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  const parseSample = (): { ok: true; value: unknown } | { ok: false } => {
    const result = parseJsonText(sample);
    if (result.ok) return result;
    pushResult({ title: "示例输入不是合法 JSON", tone: "error", text: result.message });
    return { ok: false };
  };

  const dryRun = async () => {
    if (!selection || selection.target !== "model") return;
    const input = parseSample();
    if (!input.ok || !(await ensureSaved())) return;
    const modelKey = readConfigKey(parsed.ok ? parsed.value : null) || key;
    if (!modelKey) return;
    setBusy("dry-run");
    try {
      const result = await dryRunModel(modelKey, input.value, useProviderDraft);
      pushResult({
        title: "dry-run：渲染后的请求（未发送，凭证已脱敏）",
        tone: "success",
        json: result,
      });
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  const testRun = async () => {
    if (!selection || selection.target !== "model") return;
    const input = parseSample();
    if (!input.ok || !(await ensureSaved())) return;
    const modelKey = readConfigKey(parsed.ok ? parsed.value : null) || key;
    if (!modelKey) return;
    setBusy("test-run");
    try {
      const view = await testRunModel(modelKey, input.value, useProviderDraft);
      const entryId = pushResult({
        title: `试跑任务 #${view.id}：${view.status}`,
        tone: "info",
        text: "真实调用平台，不扣用户积分。每 3 秒刷新一次状态。",
        json: view,
      });
      void pollTestRun(entryId, view.id);
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  const pollTestRun = async (entryId: string, taskId: number | string) => {
    const startedAt = Date.now();
    while (aliveRef.current && Date.now() - startedAt < TEST_POLL_MAX) {
      await new Promise((resolve) => setTimeout(resolve, TEST_POLL_INTERVAL));
      if (!aliveRef.current) return;
      try {
        const view = await getTestRun(taskId);
        const terminal = isTerminalStatus(view.status);
        patchResult(entryId, {
          title: `试跑任务 #${taskId}：${view.status}`,
          tone: terminal ? (view.status === "succeeded" ? "success" : "error") : "info",
          json: view,
          text: terminal
            ? view.error_message || (view.status === "succeeded" ? "试跑成功" : undefined)
            : "每 3 秒刷新一次状态。",
        });
        if (terminal) return;
      } catch {
        patchResult(entryId, { tone: "error", text: "查询试跑状态失败" });
        return;
      }
    }
  };

  const publish = async () => {
    if (!selection || !key && !isNew) return;
    if (!(await ensureSaved())) return;
    const publishKey = key ?? readConfigKey(parsed.ok ? parsed.value : null);
    if (!publishKey) return;
    if (!window.confirm(`确认发布 ${TARGET_LABEL[selection.target]} ${publishKey}？发布后立即对所有用户生效。`)) return;
    setBusy("publish");
    try {
      const revision = await publishConfig(selection.target, publishKey);
      pushResult({ title: `已发布（第 ${revision.revision_no} 版）`, tone: "success" });
      void loadLists();
      void openDetail(selection.target, publishKey);
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  const loadRevisions = async () => {
    if (!selection || !key) return;
    try {
      setRevisions(await listRevisions(selection.target, key));
    } catch {
      setRevisions([]);
    }
  };

  const rollback = async (revision: ConfigRevision) => {
    if (!selection || !key) return;
    if (!window.confirm(`确认回滚到第 ${revision.revision_no} 版？回滚后立即生效，进行中的任务不受影响。`)) return;
    setBusy("rollback");
    try {
      await rollbackConfig(selection.target, key, revision.id);
      pushResult({ title: `已回滚到第 ${revision.revision_no} 版`, tone: "success" });
      void loadLists();
      void openDetail(selection.target, key);
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  const toggleEnabled = async (item: { key: string }, enabled: boolean) => {
    await setModelEnabled(item.key, enabled);
    pushResult({ title: `${item.key} 已${enabled ? "上架" : "下架"}`, tone: "success" });
    void loadLists();
    if (selection?.key === item.key) void openDetail("model", item.key);
  };

  const importFromRunningHub = async () => {
    const id = webappId.trim();
    if (!/^\d+$/.test(id)) {
      pushResult({ title: "webappId 只能是数字", tone: "error" });
      return;
    }
    setBusy("import");
    try {
      const result = await importRunningHub(id);
      startNew("model", result.draft);
      setTab("config");
      pushResult({
        title: `已导入 webappId ${id} 的 ${result.nodes.length} 个可改节点`,
        tone: "success",
        text: "已生成模型草稿（尚未保存）：请确认字段含义、填写名称与积分后保存。",
        nodes: result.nodes,
        warnings: result.warnings,
      });
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  if (listState === "forbidden") {
    return (
      <main className="grid min-h-svh place-items-center p-6">
        <div className="flex max-w-sm flex-col items-center gap-3 text-center">
          <ShieldAlert className="text-destructive size-10" />
          <h1 className="text-xl font-semibold">403 没有权限</h1>
          <p className="text-muted-foreground text-sm">
            AI 配置管理只对管理员开放。如果你需要访问，请联系管理员为你的账号开通权限。
          </p>
          <Link to="/" className="text-primary text-sm hover:underline">
            返回我的画布
          </Link>
        </div>
      </main>
    );
  }

  const currentList = lists[target];
  const isModel = selection?.target === "model";
  const working = busy !== null;

  return (
    <div className="flex h-svh flex-col">
      <header className="flex h-12 shrink-0 items-center gap-3 border-b px-3">
        <Link
          to="/"
          className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm"
        >
          <ArrowLeft className="size-4" />
          画布
        </Link>
        <h1 className="text-sm font-medium">AI 配置管理</h1>
        <div className="bg-muted flex gap-0.5 rounded-lg p-0.5 text-xs">
          {(["config", "secrets"] as const).map((value) => (
            <button
              key={value}
              type="button"
              className={cn(
                "rounded-md px-3 py-1",
                tab === value ? "bg-background shadow-sm" : "text-muted-foreground",
              )}
              onClick={() => setTab(value)}
            >
              {value === "config" ? "平台与模型" : "凭证设置"}
            </button>
          ))}
        </div>
        <div className="ml-auto flex items-center gap-2">
          <Input
            value={webappId}
            onChange={(event) => setWebappId(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.nativeEvent.isComposing) void importFromRunningHub();
            }}
            placeholder="粘贴 RunningHub webappId"
            aria-label="RunningHub webappId"
            className="h-8 w-56 text-xs"
          />
          <Button
            size="sm"
            variant="outline"
            disabled={busy === "import" || !webappId.trim()}
            onClick={() => void importFromRunningHub()}
          >
            {busy === "import" ? <Loader2 className="animate-spin" /> : <Wand2 />}
            导入
          </Button>
        </div>
      </header>

      {tab === "secrets" ? (
        <div className="min-h-0 flex-1 overflow-y-auto">
          <SecretsPanel />
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
          {/* 左：平台 / 模型列表 */}
          <nav className="flex max-h-56 w-full shrink-0 flex-col border-b lg:max-h-none lg:w-64 lg:border-r lg:border-b-0">
            <div className="flex items-center gap-2 border-b p-2">
              <div className="bg-muted flex flex-1 gap-0.5 rounded-lg p-0.5 text-xs">
                {(["model", "provider"] as const).map((value) => (
                  <button
                    key={value}
                    type="button"
                    className={cn(
                      "flex-1 rounded-md px-2 py-1",
                      target === value ? "bg-background shadow-sm" : "text-muted-foreground",
                    )}
                    onClick={() => setTarget(value)}
                  >
                    {TARGET_LABEL[value]}（{lists[value].length}）
                  </button>
                ))}
              </div>
              <Button size="icon-sm" variant="outline" aria-label={`新建${TARGET_LABEL[target]}`} onClick={() => startNew(target)}>
                <Plus />
              </Button>
            </div>
            <ul className="min-h-0 flex-1 overflow-y-auto p-1.5">
              {listState === "loading" && (
                <li className="text-muted-foreground p-3 text-xs">加载中…</li>
              )}
              {listState === "error" && (
                <li className="text-destructive p-3 text-xs">
                  加载失败，
                  <button type="button" className="underline" onClick={() => void loadLists()}>
                    重试
                  </button>
                </li>
              )}
              {listState === "ready" && currentList.length === 0 && (
                <li className="text-muted-foreground p-3 text-xs">
                  还没有{TARGET_LABEL[target]}，点右上角 + 新建，或用顶部的 webappId 导入。
                </li>
              )}
              {currentList.map((item) => {
                const active = selection?.target === target && selection.key === item.key;
                return (
                  <li key={item.key}>
                    <button
                      type="button"
                      className={cn(
                        "hover:bg-muted flex w-full flex-col gap-1 rounded-lg px-2.5 py-2 text-left",
                        active && "bg-muted",
                      )}
                      onClick={() => void openDetail(target, item.key)}
                    >
                      <span className="truncate font-mono text-xs">{item.key}</span>
                      <span className="text-muted-foreground flex flex-wrap items-center gap-1.5 text-[11px]">
                        {item.name && <span>{item.name}</span>}
                        {item.kind && <span>{item.kind}</span>}
                        {item.published_revision_no != null ? (
                          <span>已发布 v{item.published_revision_no}</span>
                        ) : (
                          <span>未发布</span>
                        )}
                        {item.has_unpublished_draft && (
                          <span className="text-amber-600 dark:text-amber-400">有未发布草稿</span>
                        )}
                        {target === "model" && (
                          <span className={item.enabled ? "text-emerald-600 dark:text-emerald-400" : ""}>
                            {item.enabled ? "已上架" : "未上架"}
                          </span>
                        )}
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          </nav>

          {/* 中：JSON 编辑器 */}
          <section className="flex min-h-96 min-w-0 flex-1 flex-col">
            {!selection ? (
              <div className="text-muted-foreground grid flex-1 place-items-center p-6 text-sm">
                从左侧选一个{TARGET_LABEL[target]}，或新建一个。
              </div>
            ) : (
              <>
                <div className="flex flex-wrap items-center gap-2 border-b p-2">
                  <span className="mr-1 text-sm font-medium">
                    {TARGET_LABEL[selection.target]} ·{" "}
                    <span className="font-mono">{key ?? "（新建，未保存）"}</span>
                  </span>
                  {dirty && <span className="text-xs text-amber-600 dark:text-amber-400">有未保存修改</span>}
                  {isModel && detail && key && (
                    <label className="text-muted-foreground ml-1 flex items-center gap-1.5 text-xs">
                      <Switch
                        size="sm"
                        checked={!!detail.enabled}
                        disabled={working}
                        onCheckedChange={(checked) => void toggleEnabled({ key }, checked)}
                      />
                      {detail.enabled ? "已上架" : "未上架"}
                    </label>
                  )}
                  <div className="ml-auto flex flex-wrap items-center gap-1.5">
                    <Button size="xs" variant="ghost" onClick={format}>
                      <Braces />
                      格式化
                    </Button>
                    <Button size="xs" variant="outline" disabled={working} onClick={() => void save()}>
                      {busy === "save" ? <Loader2 className="animate-spin" /> : <Save />}
                      保存草稿
                    </Button>
                    <Button size="xs" variant="outline" disabled={working} onClick={() => void validate()}>
                      {busy === "validate" ? <Loader2 className="animate-spin" /> : <CheckCheck />}
                      校验
                    </Button>
                    {isModel && (
                      <>
                        <Button size="xs" variant="outline" disabled={working} onClick={() => void dryRun()}>
                          {busy === "dry-run" ? <Loader2 className="animate-spin" /> : <FlaskConical />}
                          dry-run
                        </Button>
                        <Button size="xs" variant="outline" disabled={working} onClick={() => void testRun()}>
                          {busy === "test-run" ? <Loader2 className="animate-spin" /> : <Play />}
                          试跑
                        </Button>
                      </>
                    )}
                    <Button size="xs" disabled={working || isNew} onClick={() => void publish()}>
                      {busy === "publish" ? <Loader2 className="animate-spin" /> : <Rocket />}
                      发布
                    </Button>
                    <DropdownMenu
                      modal={false}
                      onOpenChange={(open) => {
                        if (open) void loadRevisions();
                      }}
                    >
                      <DropdownMenuTrigger
                        disabled={working || isNew}
                        className="border-border bg-background hover:bg-muted inline-flex h-7 items-center gap-1.5 rounded-[min(var(--radius-md),10px)] border px-2 text-xs font-medium disabled:opacity-50"
                      >
                        <Undo2 className="size-3.5" />
                        回滚
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end" className="w-72" sideOffset={6}>
                        <DropdownMenuGroup>
                          <DropdownMenuLabel>回滚到历史版本</DropdownMenuLabel>
                          {revisions === null && (
                            <p className="text-muted-foreground px-1.5 py-2 text-xs">加载中…</p>
                          )}
                          {revisions?.length === 0 && (
                            <p className="text-muted-foreground px-1.5 py-2 text-xs">没有可回滚的历史版本</p>
                          )}
                          {revisions
                            ?.filter((revision) => revision.status !== "draft")
                            .map((revision) => (
                              <DropdownMenuItem key={revision.id} onClick={() => void rollback(revision)}>
                                <History />
                                <span className="flex min-w-0 flex-1 flex-col">
                                  <span>
                                    第 {revision.revision_no} 版 ·{" "}
                                    {revision.status === "published" ? "当前发布" : "历史"}
                                  </span>
                                  <span className="text-muted-foreground truncate text-xs">
                                    {new Date(revision.created_at).toLocaleString()}
                                    {revision.note ? ` · ${revision.note}` : ""}
                                  </span>
                                </span>
                              </DropdownMenuItem>
                            ))}
                        </DropdownMenuGroup>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                </div>

                <textarea
                  value={text}
                  onChange={(event) => setText(event.target.value)}
                  spellCheck={false}
                  wrap="off"
                  aria-label="配置 JSON"
                  aria-invalid={!parsed.ok}
                  className="bg-background min-h-64 flex-1 resize-none overflow-auto p-3 font-mono text-xs leading-5 outline-none"
                  onKeyDown={(event) => {
                    // Tab 插入两个空格，别把焦点跳走
                    if (event.key !== "Tab" || event.shiftKey) return;
                    event.preventDefault();
                    const el = event.currentTarget;
                    const { selectionStart, selectionEnd } = el;
                    setText(`${text.slice(0, selectionStart)}  ${text.slice(selectionEnd)}`);
                    requestAnimationFrame(() => {
                      el.selectionStart = el.selectionEnd = selectionStart + 2;
                    });
                  }}
                />
                <div
                  className={cn(
                    "border-t px-3 py-1.5 text-xs",
                    parsed.ok ? "text-muted-foreground" : "text-destructive",
                  )}
                  role={parsed.ok ? undefined : "alert"}
                >
                  {parsed.ok
                    ? "JSON 语法正确（结构与表达式由「校验」检查）"
                    : `JSON 语法错误${parsed.line ? `（第 ${parsed.line} 行 第 ${parsed.column} 列）` : ""}：${parsed.message}`}
                </div>

                {isModel && (
                  <div className="flex flex-col gap-1.5 border-t p-2">
                    <div className="flex items-center justify-between text-xs">
                      <span className="text-muted-foreground">
                        示例输入（dry-run / 试跑用，按 input_schema 字段名，媒体字段填素材 id）
                      </span>
                      <label className="text-muted-foreground flex items-center gap-1.5">
                        <input
                          type="checkbox"
                          checked={useProviderDraft}
                          onChange={(event) => setUseProviderDraft(event.target.checked)}
                        />
                        用平台草稿
                      </label>
                    </div>
                    <textarea
                      value={sample}
                      onChange={(event) => setSample(event.target.value)}
                      spellCheck={false}
                      aria-label="示例输入 JSON"
                      className="border-input bg-transparent h-20 resize-none rounded-lg border p-2 font-mono text-xs outline-none focus-visible:border-ring"
                    />
                  </div>
                )}
              </>
            )}
          </section>

          {/* 右：结果 */}
          <ResultPanel entries={results} onClear={() => setResults([])} />
        </div>
      )}
    </div>
  );
}
