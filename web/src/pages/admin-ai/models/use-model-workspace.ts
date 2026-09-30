import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";

import {
  createModelDraft,
  dryRunModel,
  getModelDetail,
  getTestRun,
  getTestRunTrace,
  listModelRevisions,
  listModels,
  publishModel,
  rollbackModel,
  setModelEnabled,
  testRunModel,
  updateModelDraft,
  validateModel,
} from "@/api/admin-ai";
import type {
  ConfigDetail,
  ConfigIssue,
  ConfigListItem,
  ConfigRevision,
} from "@/api/admin-ai/type";
import { errorMessage, isRunnerDown } from "@/utils/admin/errors";
import { readStashedDrafts, updateStashedDrafts } from "@/utils/admin/import-draft";
import { formatJsonText, parseJsonText, readConfigKey, toJsonText } from "@/utils/admin/json";
import { resolveModelChannel, publishBlockReason } from "@/utils/admin/model-channel";
import { isTerminalStatus } from "@/utils/tasks/status";

import type { DryRunState, ResultEntry, ResultTabId, RunState, TraceState } from "../result-panel";
import { MODEL_TEMPLATES } from "../templates";
import type { AdminCatalog } from "../use-admin";
import { useAliveRef } from "../use-admin";

/** 编辑器的两种视图 */
export type EditorMode = "form" | "json";

const MAX_ENTRIES = 30;
/** 试跑状态轮询间隔（毫秒） */
const TEST_POLL_INTERVAL = 3000;
/** 试跑轮询上限：15 分钟，超过后不标失败，提示稍后再看 */
const TEST_POLL_MAX = 15 * 60 * 1000;

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

/**
 * 模型页的全部状态与动作：列表、选中、JSON 正文（事实来源）、表单 / JSON 视图切换、
 * 保存 / 校验 / dry-run / 试跑 / 发布 / 回滚 / 上下架、导入草稿队列、结果面板与追踪。
 * 选中的模型由 URL 决定（?key=<key>，/models/new 是新建），所以刷新与深链都能还原。
 * 请求失败的全局 toast 由拦截器统一弹；这里 catch 只做状态恢复或把原因翻译成就地提示。
 */
export function useModelWorkspace(catalog: AdminCatalog) {
  const aliveRef = useAliveRef();
  const navigate = useNavigate();
  const location = useLocation();
  const [params] = useSearchParams();

  const isNewRoute = location.pathname.replace(/\/+$/, "").endsWith("/models/new");
  const key = isNewRoute ? null : params.get("key");
  const draftId = isNewRoute ? params.get("draft") : null;
  const fromImport = isNewRoute && params.get("from") === "import";
  const selection: "none" | "new" | "model" = isNewRoute ? "new" : key ? "model" : "none";
  const isNew = selection === "new";

  // ---- 列表
  const [models, setModels] = useState<ConfigListItem[]>([]);
  const [listState, setListState] = useState<"loading" | "ready" | "error">("loading");

  // ---- 编辑器
  const [detail, setDetail] = useState<ConfigDetail | null>(null);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [text, setText] = useState("");
  const [savedText, setSavedText] = useState("");
  const [sample, setSample] = useState("{}");
  const [mode, setMode] = useState<EditorMode>("form");
  const [modeError, setModeError] = useState<string | null>(null);
  /** 正文被整体替换（切换模型、切回表单等）的计数，表单里的 JSON 编辑框据此重新挂载 */
  const [epoch, setEpoch] = useState(0);
  const [issues, setIssues] = useState<ConfigIssue[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [revisions, setRevisions] = useState<ConfigRevision[] | null>(null);

  // ---- 结果面板
  const [entries, setEntries] = useState<ResultEntry[]>([]);
  const [dryRun, setDryRun] = useState<DryRunState | null>(null);
  const [run, setRun] = useState<RunState | null>(null);
  const [trace, setTrace] = useState<TraceState | null>(null);
  const [resultTab, setResultTab] = useState<ResultTabId>("issues");

  // ---- 对话框
  const [publishKey, setPublishKey] = useState<string | null>(null);
  const [publishError, setPublishError] = useState<string | null>(null);
  const [rollbackTarget, setRollbackTarget] = useState<ConfigRevision | null>(null);
  const [rollbackError, setRollbackError] = useState<string | null>(null);
  const [pendingNav, setPendingNav] = useState<(() => void) | null>(null);

  // ---- 导入草稿队列（队首就是编辑器里当前的那个）
  const [queue, setQueue] = useState<Record<string, unknown>[]>([]);
  const importRef = useRef<{ id: string; channelKey: string } | null>(null);

  const runTokenRef = useRef(0);
  const latestKeyRef = useRef<string | null>(null);
  /** 刚由“新建保存”跳转到的 key：跳转后的加载不要清掉刚写进结果面板的记录 */
  const keepResultsForRef = useRef<string | null>(null);

  const parsed = useMemo(() => parseJsonText(text), [text]);
  const body = parsed.ok && isRecord(parsed.value) ? parsed.value : null;
  const dirty = text !== savedText;
  const bodyKey = body ? readConfigKey(body) : "";
  const modelKey = key ?? bodyKey;
  const info = useMemo(
    () => resolveModelChannel(body, catalog.channels, catalog.plugins),
    [body, catalog.channels, catalog.plugins],
  );
  const publishBlock = publishBlockReason(info, catalog.channelsStatus === "ready");
  const working = busy !== null;

  // ------------------------------------------------------------ 结果面板

  const pushEntry = useCallback((entry: Omit<ResultEntry, "id" | "time">, focus = true) => {
    const id = crypto.randomUUID();
    setEntries((prev) => [{ ...entry, id, time: Date.now() }, ...prev].slice(0, MAX_ENTRIES));
    if (focus) setResultTab("issues");
    return id;
  }, []);

  const resetResults = useCallback(() => {
    runTokenRef.current += 1;
    setEntries([]);
    setDryRun(null);
    setRun(null);
    setTrace(null);
    setIssues([]);
    setResultTab("issues");
  }, []);

  const clearResults = resetResults;

  // ------------------------------------------------------------ 列表与详情

  const loadList = useCallback(async () => {
    try {
      const list = await listModels();
      if (!aliveRef.current) return;
      setModels(list);
      setListState("ready");
    } catch {
      if (aliveRef.current) setListState((prev) => (prev === "ready" ? prev : "error"));
    }
  }, [aliveRef]);

  useEffect(() => {
    void loadList();
  }, [loadList]);

  /** 把一份正文放进编辑器：JSON 文本、已保存基线、视图与 JSON 编辑框一并重置 */
  const loadIntoEditor = useCallback((next: unknown) => {
    const content = toJsonText(next);
    setText(content);
    setSavedText(content);
    setModeError(null);
    setEpoch((value) => value + 1);
  }, []);

  const openDetail = useCallback(
    async (nextKey: string, keepResults = false) => {
      latestKeyRef.current = nextKey;
      if (!keepResults) resetResults();
      setRevisions(null);
      setLoadingDetail(true);
      try {
        const data = await getModelDetail(nextKey);
        if (!aliveRef.current || latestKeyRef.current !== nextKey) return;
        setDetail(data);
        loadIntoEditor(data.draft?.body_json ?? data.published?.body_json ?? {});
      } catch {
        // 全局 toast 已弹；详情取不到就清空编辑器，避免拿上一个模型的内容去保存
        if (aliveRef.current && latestKeyRef.current === nextKey) {
          setDetail(null);
          setText("");
          setSavedText("");
        }
      } finally {
        if (aliveRef.current && latestKeyRef.current === nextKey) setLoadingDetail(false);
      }
    },
    [aliveRef, loadIntoEditor, resetResults],
  );

  /** 进入“新建”：有导入草稿就取出队列并预填，否则用空白模板 */
  const enterNew = useCallback(() => {
    latestKeyRef.current = null;
    resetResults();
    setDetail(null);
    setRevisions(null);
    setLoadingDetail(false);
    const stashed = readStashedDrafts(draftId);
    if (stashed) {
      importRef.current = { id: draftId as string, channelKey: stashed.channelKey };
      setQueue(stashed.bodies);
      loadIntoEditor(stashed.bodies[0]);
      pushEntry({
        title: `已带入 ${stashed.bodies.length} 个草稿`,
        tone: "info",
        text: "导入的只是草稿，不会上架。逐个确认、保存、试跑后再发布。",
      });
      return;
    }
    importRef.current = null;
    setQueue([]);
    loadIntoEditor(MODEL_TEMPLATES[0].body);
    if (fromImport) {
      pushEntry({
        title: "没有读到导入的草稿",
        tone: "info",
        text: "浏览器没能暂存草稿（可能是隐私模式或页面已刷新），已用空白模板，请手动填写。",
      });
    }
  }, [draftId, fromImport, loadIntoEditor, pushEntry, resetResults]);

  useEffect(() => {
    if (selection === "model" && key) {
      const keep = keepResultsForRef.current === key;
      keepResultsForRef.current = null;
      void openDetail(key, keep);
    } else if (selection === "new") {
      enterNew();
    } else {
      latestKeyRef.current = null;
      resetResults();
      setDetail(null);
      setText("");
      setSavedText("");
      setLoadingDetail(false);
    }
    // 只在选中对象变化时执行；openDetail / enterNew 引用稳定性由各自依赖保证
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selection, key, draftId]);

  // ------------------------------------------------------------ 导航守卫

  /** 有未保存修改时先确认再执行导航动作 */
  const guard = useCallback(
    (action: () => void) => {
      if (dirty && selection !== "none") setPendingNav(() => action);
      else action();
    },
    [dirty, selection],
  );

  const confirmNav = () => {
    const action = pendingNav;
    setPendingNav(null);
    action?.();
  };

  const selectModel = (nextKey: string) =>
    guard(() => navigate(`/admin/ai/models?key=${encodeURIComponent(nextKey)}`));

  const startNew = () =>
    guard(() => {
      if (isNewRoute && !draftId && !fromImport) {
        // 已经在新建页：直接重置成空白模板
        importRef.current = null;
        setQueue([]);
        loadIntoEditor(MODEL_TEMPLATES[0].body);
        resetResults();
        return;
      }
      navigate("/admin/ai/models/new");
    });

  // ------------------------------------------------------------ 编辑

  /** 表单改字段：在 JSON 正文上原位修改，保持键顺序；正文不合法时不动 */
  const editBody = useCallback(
    (mutate: (current: Record<string, unknown>) => Record<string, unknown> | null) => {
      setText((prev) => {
        const result = parseJsonText(prev);
        if (!result.ok || !isRecord(result.value)) return prev;
        const next = mutate(result.value);
        return next ? toJsonText(next) : prev;
      });
    },
    [],
  );

  /** 切换表单 / JSON；JSON 解析失败时阻止切回表单并提示行列 */
  const switchMode = (next: EditorMode) => {
    if (next === mode) return;
    if (next === "form") {
      if (!parsed.ok) {
        const where = parsed.line ? `第 ${parsed.line} 行 第 ${parsed.column} 列` : "";
        setModeError(
          `JSON 有语法错误${where ? `（${where}）` : ""}：${parsed.message}。修复后才能切回表单。`,
        );
        return;
      }
      if (!isRecord(parsed.value)) {
        setModeError("正文必须是 JSON 对象，才能切回表单。");
        return;
      }
      setEpoch((value) => value + 1);
    }
    setModeError(null);
    setMode(next);
  };

  const format = () => {
    const result = formatJsonText(text);
    if (result.ok) setText(result.text);
    else pushEntry({ title: "无法格式化", tone: "error", text: result.message });
  };

  // ------------------------------------------------------------ 动作

  /**
   * 保存草稿；成功返回模型 key 与校验问题。有校验问题也照常保存（后端允许存半成品），只是发布会被拦。
   * 新建成功后：导入队列里还有草稿就接着处理下一个，否则跳到该模型的页面。
   */
  const save = async (silent = false): Promise<{ key: string; issues: ConfigIssue[] } | null> => {
    if (selection === "none") return null;
    if (!parsed.ok) {
      pushEntry({ title: "JSON 格式错误，无法保存", tone: "error", text: parsed.message });
      return null;
    }
    const newKey = readConfigKey(parsed.value);
    if (!newKey) {
      pushEntry({
        title: "正文里缺少 key",
        tone: "error",
        text: "key 是配置的唯一标识，必须填写。",
      });
      return null;
    }
    const sentText = text;
    setBusy("save");
    setIssues([]);
    try {
      const result = isNew
        ? await createModelDraft(parsed.value)
        : await updateModelDraft(key as string, parsed.value);
      if (!aliveRef.current) return null;
      setSavedText(sentText);
      setIssues(result.issues);
      if (!silent || result.issues.length > 0) {
        pushEntry({
          title: `草稿已保存（第 ${result.revision.revision_no} 版）`,
          tone: result.issues.length > 0 ? "info" : "success",
          text:
            result.issues.length > 0
              ? `有 ${result.issues.length} 个问题，修复后才能发布。`
              : undefined,
          issues: result.issues,
        });
      }
      void loadList();
      if (isNew) {
        const rest = queue.slice(1);
        if (queue.length > 0 && rest.length > 0) {
          setQueue(rest);
          if (importRef.current)
            updateStashedDrafts(importRef.current.id, {
              channelKey: importRef.current.channelKey,
              bodies: rest,
            });
          loadIntoEditor(rest[0]);
          pushEntry({
            title: `已保存 ${newKey}，接着处理下一个草稿`,
            tone: "success",
            text: `还剩 ${rest.length} 个。`,
          });
        } else {
          if (importRef.current)
            updateStashedDrafts(importRef.current.id, {
              channelKey: importRef.current.channelKey,
              bodies: [],
            });
          setQueue([]);
          keepResultsForRef.current = newKey;
          navigate(`/admin/ai/models?key=${encodeURIComponent(newKey)}`, { replace: true });
        }
      }
      return { key: newKey, issues: result.issues };
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  /** dry-run / 试跑 / 发布都基于服务端已保存的草稿，有未保存改动先存；返回可用的模型 key */
  const ensureSaved = async (): Promise<{ key: string; issues: ConfigIssue[] } | null> => {
    if (dirty || isNew) return save(true);
    const existing = key ?? bodyKey;
    return existing ? { key: existing, issues: [] } : null;
  };

  const validate = async () => {
    if (selection === "none") return;
    if (!parsed.ok) {
      pushEntry({ title: "JSON 格式错误", tone: "error", text: parsed.message });
      return;
    }
    const target = key ?? readConfigKey(parsed.value);
    if (!target) {
      pushEntry({
        title: "正文里缺少 key",
        tone: "error",
        text: "key 是配置的唯一标识，必须填写。",
      });
      return;
    }
    setBusy("validate");
    try {
      const result = await validateModel(target, parsed.value);
      setIssues(result.issues);
      pushEntry({
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
    pushEntry({ title: "示例输入不是合法 JSON", tone: "error", text: result.message });
    return { ok: false };
  };

  const doDryRun = async () => {
    if (selection === "none") return;
    const input = parseSample();
    if (!input.ok) return;
    const saved = await ensureSaved();
    if (!saved) return;
    setBusy("dry-run");
    try {
      const result = await dryRunModel(saved.key, input.value);
      setDryRun({ time: Date.now(), json: result });
      setResultTab("dry-run");
    } catch (error) {
      // runner 不可用：就地说明，保留输入可重试；其余错误只靠全局 toast
      if (aliveRef.current && isRunnerDown(error)) {
        setDryRun({ time: Date.now(), error: "插件运行器暂时不可用，稍后重试。示例输入已保留。" });
        setResultTab("dry-run");
      }
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  const loadTrace = useCallback(
    async (taskId: number | string) => {
      setTrace((prev) => ({
        taskId,
        status: "loading",
        steps: prev?.taskId === taskId ? prev.steps : [],
      }));
      try {
        const result = await getTestRunTrace(taskId);
        if (aliveRef.current) setTrace({ taskId, status: "ready", steps: result.steps });
      } catch {
        if (aliveRef.current) setTrace({ taskId, status: "error", steps: [] });
      }
    },
    [aliveRef],
  );

  /** 每 3 秒查一次试跑状态，最长 15 分钟；结束（任何终态）后拉追踪并自动切到追踪标签 */
  const pollTestRun = async (token: number, taskId: number | string) => {
    const startedAt = Date.now();
    while (
      aliveRef.current &&
      runTokenRef.current === token &&
      Date.now() - startedAt < TEST_POLL_MAX
    ) {
      await new Promise((resolve) => setTimeout(resolve, TEST_POLL_INTERVAL));
      if (!aliveRef.current || runTokenRef.current !== token) return;
      try {
        const view = await getTestRun(taskId);
        if (runTokenRef.current !== token) return;
        setRun({ taskId, view, timedOut: false });
        if (isTerminalStatus(view.status)) {
          await loadTrace(taskId);
          if (aliveRef.current && runTokenRef.current === token) setResultTab("trace");
          return;
        }
      } catch {
        // 全局 toast 已弹；停止轮询并在面板里说明
        if (aliveRef.current && runTokenRef.current === token) {
          setRun((prev) => (prev ? { ...prev, note: "查询试跑状态失败，已停止刷新。" } : prev));
        }
        return;
      }
    }
    if (aliveRef.current && runTokenRef.current === token) {
      setRun((prev) => (prev ? { ...prev, timedOut: true } : prev));
    }
  };

  const doTestRun = async () => {
    if (selection === "none") return;
    const input = parseSample();
    if (!input.ok) return;
    const saved = await ensureSaved();
    if (!saved) return;
    setBusy("test-run");
    try {
      const view = await testRunModel(saved.key, input.value);
      if (!aliveRef.current) return;
      runTokenRef.current += 1;
      const token = runTokenRef.current;
      setRun({ taskId: view.id, view, timedOut: false });
      setTrace(null);
      setResultTab("run");
      void pollTestRun(token, view.id);
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  const refreshTrace = () => {
    const taskId = trace?.taskId ?? run?.taskId;
    if (taskId !== undefined) void loadTrace(taskId);
  };

  /** 点“发布”：先保存，有校验问题就留在问题标签；否则打开确认框 */
  const requestPublish = async () => {
    if (selection === "none") return;
    const saved = await ensureSaved();
    if (!saved) return;
    if (saved.issues.length > 0) {
      setResultTab("issues");
      return;
    }
    setPublishError(null);
    setPublishKey(saved.key);
  };

  const confirmPublish = async () => {
    if (!publishKey) return;
    setBusy("publish");
    setPublishError(null);
    try {
      const revision = await publishModel(publishKey);
      if (!aliveRef.current) return;
      pushEntry({ title: `已发布（第 ${revision.revision_no} 版）`, tone: "success" });
      setPublishKey(null);
      void loadList();
      void openDetail(publishKey, true);
    } catch (error) {
      // 全局 toast 已弹；原因留在确认框里，配置校验未通过（40010）时同时回到问题标签
      if (!aliveRef.current) return;
      const message = errorMessage(error, "发布失败");
      setPublishError(message);
      if (
        typeof error === "object" &&
        error !== null &&
        (error as { code?: unknown }).code === 40010
      ) {
        pushEntry({ title: "发布被拦截：配置校验未通过", tone: "error", text: message });
      }
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  const loadRevisions = async () => {
    if (!key) return;
    try {
      setRevisions(await listModelRevisions(key));
    } catch {
      setRevisions([]);
    }
  };

  const requestRollback = (revision: ConfigRevision) => {
    setRollbackError(null);
    setRollbackTarget(revision);
  };

  const confirmRollback = async () => {
    if (!key || !rollbackTarget) return;
    setBusy("rollback");
    setRollbackError(null);
    try {
      await rollbackModel(key, rollbackTarget.id);
      if (!aliveRef.current) return;
      pushEntry({ title: `已回滚到第 ${rollbackTarget.revision_no} 版`, tone: "success" });
      setRollbackTarget(null);
      void loadList();
      void openDetail(key, true);
    } catch (error) {
      if (aliveRef.current) setRollbackError(errorMessage(error, "回滚失败"));
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  const toggleEnabled = async (enabled: boolean) => {
    if (!key) return;
    setBusy("enabled");
    try {
      await setModelEnabled(key, enabled);
      if (!aliveRef.current) return;
      pushEntry({ title: `${key} 已${enabled ? "上架" : "下架"}`, tone: "success" }, false);
      void loadList();
      setDetail((prev) => (prev ? { ...prev, enabled } : prev));
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  // ------------------------------------------------------------ 导入队列

  /** 跳过队首草稿，处理下一个；没有下一个就回到空白模板 */
  const skipDraft = () => {
    const rest = queue.slice(1);
    setQueue(rest);
    if (importRef.current)
      updateStashedDrafts(importRef.current.id, {
        channelKey: importRef.current.channelKey,
        bodies: rest,
      });
    loadIntoEditor(rest[0] ?? MODEL_TEMPLATES[0].body);
  };

  const discardDrafts = () => {
    if (importRef.current)
      updateStashedDrafts(importRef.current.id, {
        channelKey: importRef.current.channelKey,
        bodies: [],
      });
    setQueue([]);
    loadIntoEditor(MODEL_TEMPLATES[0].body);
  };

  return {
    // 选中
    selection,
    key,
    isNew,
    modelKey,
    // 列表
    models,
    listState,
    reloadList: loadList,
    // 编辑器
    detail,
    loadingDetail,
    text,
    setText,
    dirty,
    parsed,
    body,
    sample,
    setSample,
    mode,
    modeError,
    switchMode,
    epoch,
    issues,
    editBody,
    format,
    // 状态
    busy,
    working,
    info,
    publishBlock,
    // 动作
    selectModel,
    startNew,
    save,
    validate,
    doDryRun,
    doTestRun,
    requestPublish,
    confirmPublish,
    requestRollback,
    confirmRollback,
    toggleEnabled,
    loadRevisions,
    revisions,
    refreshTrace,
    // 结果
    entries,
    dryRun,
    run,
    trace,
    resultTab,
    setResultTab,
    clearResults,
    pushEntry,
    // 对话框
    publishKey,
    publishError,
    cancelPublish: () => setPublishKey(null),
    rollbackTarget,
    rollbackError,
    cancelRollback: () => setRollbackTarget(null),
    pendingNav,
    confirmNav,
    cancelNav: () => setPendingNav(null),
    // 导入队列
    queue,
    skipDraft,
    discardDrafts,
  };
}

/** 工作区对象的类型，供子组件的 props 使用 */
export type ModelWorkspace = ReturnType<typeof useModelWorkspace>;
