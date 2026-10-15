import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  createModel,
  dryRunModel,
  getModelDetail,
  getTestRun,
  getTestRunTrace,
  setModelEnabled,
  testRunModel,
  updateModel,
  validateModel,
} from "@/api/admin/ai";
import type { ChannelOption, ConfigDetail, ConfigIssue, PluginView } from "@/api/admin/ai/type.d";
import { isRunnerDown } from "@/utils/admin/errors";
import { formatJsonText, parseJsonText, readConfigKey, toJsonText } from "@/utils/admin/json";
import { resolveModelChannel } from "@/utils/admin/model-channel";
import { blankModelBody } from "@/utils/admin/model-template";
import { toast } from "sonner";

import type { Capabilities } from "@/api/model/type.d";
import type { ParamAsset } from "@/types";
import { buildTaskInput, manualRefs, type RefKey } from "@/utils/tasks/capabilities";
import { isTerminalStatus } from "@/utils/tasks/status";

import type { DryRunState, ResultNotice, ResultTabId, RunState, TraceState } from "../result-panel";
import type { LoadStatus } from "../../use-admin";
import { useAliveRef } from "../../use-admin";
import { modelChecks, samePlace } from "./model-fields";
import type { ModelRoute } from "./model-route";

/** 模型编辑器要用的渠道与插件清单（AdminCatalog 的子集，面板由宿主传入） */
export type ModelCatalog = {
  /** 全部渠道：“所属渠道”下拉与启用前提示用 */
  channels: ChannelOption[];
  /** 全部插件（带全部版本和 meta）：协议列表与协议设置项表单用 */
  plugins: PluginView[];
  /** 插件清单的加载状态：没加载好时不让切能力 */
  pluginsStatus: LoadStatus;
};

/** 编辑器的两种视图 */
export type EditorMode = "form" | "json";

/** 试跑状态轮询间隔（毫秒） */
const TEST_POLL_INTERVAL = 3000;
/** 试跑轮询上限：15 分钟，超过后不标失败，提示稍后再看 */
const TEST_POLL_MAX = 15 * 60 * 1000;

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

/**
 * 模型编辑器的全部状态与动作：选中、JSON 正文（事实来源）、表单 / JSON 视图切换、
 * 保存 / 校验 / dry-run / 试跑 / 上线下线、结果面板与追踪。
 * 选中的模型由 route 决定（放在模型管理面板自己的 state 里）。
 * 请求失败的全局 toast 由拦截器统一弹；这里 catch 只做状态恢复或把原因翻译成就地提示。
 */
export function useModelWorkspace(
  catalog: ModelCatalog,
  route: ModelRoute,
  /** 保存 / 上下线让模型列表变了之后回调（宿主页面有自己的模型列表时用来刷新） */
  onListChanged?: () => void,
) {
  const aliveRef = useAliveRef();
  const onListChangedRef = useRef(onListChanged);
  onListChangedRef.current = onListChanged;

  const { key, presetChannel } = route;
  const selection: "none" | "new" | "model" = route.isNew ? "new" : key ? "model" : "none";
  const isNew = selection === "new";

  // ---- 列表

  // ---- 编辑器
  const [detail, setDetail] = useState<ConfigDetail | null>(null);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [text, setText] = useState("");
  const [savedText, setSavedText] = useState("");
  /** 测试节点上填的参数（字段名 -> 值），和画布节点的 params 同一形状 */
  const [testParams, setTestParams] = useState<Record<string, unknown>>({});
  /** 测试节点里选的素材，只用于展示 */
  const [testAssets, setTestAssets] = useState<Record<string, ParamAsset>>({});
  /** 点过 dry-run / 试跑之后才标红没填的字段 */
  const [showTestErrors, setShowTestErrors] = useState(false);
  const [mode, setMode] = useState<EditorMode>("form");
  const [modeError, setModeError] = useState<string | null>(null);
  /** 正文被整体替换（切换模型、切回表单等）的计数，表单里的 JSON 编辑框据此重新挂载 */
  const [epoch, setEpoch] = useState(0);
  const [issues, setIssues] = useState<ConfigIssue[]>([]);
  const [busy, setBusy] = useState<string | null>(null);

  // ---- 结果面板
  const [dryRun, setDryRun] = useState<DryRunState | null>(null);
  const [run, setRun] = useState<RunState | null>(null);
  const [trace, setTrace] = useState<TraceState | null>(null);
  const [resultTab, setResultTab] = useState<ResultTabId>("run");

  // ---- 对话框
  const [pendingNav, setPendingNav] = useState<(() => void) | null>(null);

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
  /** 页签与底栏的提示：error 级是红点（阻止保存），warning 级是黄点 */
  const checks = useMemo(
    () => modelChecks(body, info, issues, catalog.pluginsStatus === "ready"),
    [body, info, issues, catalog.pluginsStatus],
  );
  const blockers = useMemo(() => checks.filter((check) => check.level === "error"), [checks]);
  /** 表单字段下的红字：后端返回的问题 + 实时的红点问题（同一位置只留红点那条） */
  const fieldIssues = useMemo<ConfigIssue[]>(() => {
    const live = blockers.flatMap((check) =>
      check.path ? [{ path: check.path, message: check.text }] : [],
    );
    const backend = issues.filter(
      (issue) => !live.some((item) => samePlace(item.path, issue.path)),
    );
    return [...live, ...backend];
  }, [blockers, issues]);
  /** 表单字段下的琥珀色建议（后端问题已在 fieldIssues 里） */
  const fieldWarnings = useMemo<ConfigIssue[]>(
    () =>
      modelChecks(body, info, [], catalog.pluginsStatus === "ready").flatMap((check) =>
        check.level === "warning" && check.path ? [{ path: check.path, message: check.text }] : [],
      ),
    [body, info, catalog.pluginsStatus],
  );
  const capabilities = (body && isRecord(body.capabilities) ? body.capabilities : undefined) as
    | Capabilities
    | undefined;
  const testInput = useMemo(
    () => buildTaskInput(capabilities, testParams),
    [capabilities, testParams],
  );
  const setTestParam = useCallback((name: string, value: unknown) => {
    setTestParams((prev) => {
      const next = { ...prev };
      if (value === undefined) delete next[name];
      else next[name] = value;
      return next;
    });
  }, []);
  /** 测试节点上手动加一个参考素材（上传的素材 id 存进 params.images / videos / audios） */
  const addTestRef = useCallback((key: RefKey, assetId: string | number, asset: ParamAsset) => {
    const id = String(assetId);
    setTestParams((prev) => {
      const existing = manualRefs(prev, key).map(String);
      return { ...prev, [key]: existing.includes(id) ? existing : [...existing, id] };
    });
    setTestAssets((prev) => ({ ...prev, [id]: asset }));
  }, []);
  const removeTestRef = useCallback((key: RefKey, assetId: string | number) => {
    const id = String(assetId);
    setTestParams((prev) => {
      const rest = manualRefs(prev, key)
        .map(String)
        .filter((value) => value !== id);
      const next = { ...prev };
      if (rest.length > 0) next[key] = rest;
      else delete next[key];
      return next;
    });
    setTestAssets((prev) => {
      const next = { ...prev };
      delete next[id];
      return next;
    });
  }, []);
  const working = busy !== null;

  // ------------------------------------------------------------ 结果面板

  /** 操作结果提示：用 toast 弹出，不再占结果面板；校验问题另外放在 issues 里就地显示 */
  const pushEntry = useCallback((entry: ResultNotice) => {
    const show =
      entry.tone === "success" ? toast.success : entry.tone === "error" ? toast.error : toast.info;
    show(entry.title, { description: entry.text });
  }, []);

  const resetResults = useCallback(() => {
    runTokenRef.current += 1;
    setDryRun(null);
    setRun(null);
    setTrace(null);
    setIssues([]);
    setTestParams({});
    setTestAssets({});
    setShowTestErrors(false);
    setResultTab("run");
  }, []);

  const clearResults = resetResults;

  // ------------------------------------------------------------ 列表与详情

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
      setLoadingDetail(true);
      try {
        const data = await getModelDetail(nextKey);
        if (!aliveRef.current || latestKeyRef.current !== nextKey) return;
        setDetail(data);
        loadIntoEditor(data.body ?? {});
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

  /** 进入“新建”：用“待配置”的空白正文，预选渠道（当前渠道）；能力与协议在弹窗里选 */
  const enterNew = useCallback(() => {
    latestKeyRef.current = null;
    resetResults();
    setDetail(null);
    setLoadingDetail(false);
    loadIntoEditor(blankModelBody(presetChannel ?? ""));
  }, [presetChannel, loadIntoEditor, resetResults]);

  useEffect(() => {
    if (selection === "model" && key) {
      const keep = keepResultsForRef.current === key;
      keepResultsForRef.current = null;
      void openDetail(key, keep);
    } else if (selection === "new") {
      enterNew();
    } else {
      // 关闭：先作废进行中的请求与试跑轮询；编辑器内容留到弹窗退出动画播完再清（见 clearEditor）
      latestKeyRef.current = null;
      runTokenRef.current += 1;
      setLoadingDetail(false);
    }
    // 只在选中对象变化时执行；openDetail / enterNew 引用稳定性由各自依赖保证
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selection, key]);

  const selectionRef = useRef(selection);
  selectionRef.current = selection;
  /**
   * 清空编辑器：由弹窗在退出动画播完后调用（onOpenChangeComplete），
   * 关闭过程中弹窗里仍是原来的模型，而不是先变成空白。动画期间又打开了别的模型则不清。
   */
  const clearEditor = useCallback(() => {
    if (selectionRef.current !== "none") return;
    resetResults();
    setDetail(null);
    setText("");
    setSavedText("");
  }, [resetResults]);

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

  /** 打开某个模型的编辑弹窗；test 为 true 时同时打开测试弹窗 */
  const selectModel = (nextKey: string, options?: { test?: boolean }) =>
    guard(() => route.openModel(nextKey, options));

  /** 关闭编辑弹窗回到列表；有未保存修改时先确认 */
  const closeEditor = () => guard(() => route.close());

  /** 新建模型；已经在新建时重置成空白正文 */
  const startNew = () =>
    guard(() => {
      if (route.isNew) {
        loadIntoEditor(blankModelBody(presetChannel ?? ""));
        resetResults();
        return;
      }
      route.openNew();
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
   * 保存配置；成功返回模型 key 与校验问题。新建的模型默认不上线，要在列表里打开「上线」开关用户才能用；
   * 已上线的模型保存即生效，校验不过会被后端拒绝。没上线的模型有校验问题也照常保存（允许存半成品），只是上线会被拦。
   * 新建成功后跳到该模型的编辑弹窗。
   * closeAfter：点「保存」按钮时传 true，保存成功后直接关掉弹窗回列表（已保存，不再走放弃修改确认）。
   */
  const save = async (
    silent = false,
    closeAfter = false,
  ): Promise<{ key: string; issues: ConfigIssue[] } | null> => {
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
    // 红点问题（必填项没填、配置自相矛盾）不允许保存；原因已经实时显示在对应字段上
    if (blockers.length > 0) {
      pushEntry({
        title: blockers[0].text,
        tone: "error",
      });
      return null;
    }
    const sentText = text;
    setBusy("save");
    setIssues([]);
    try {
      const result = isNew
        ? await createModel(parsed.value)
        : await updateModel(key as string, parsed.value);
      if (!aliveRef.current) return null;
      setSavedText(sentText);
      setIssues(result.issues);
      if (!silent || result.issues.length > 0) {
        pushEntry({
          title: "已保存",
          tone: result.issues.length > 0 ? "info" : "success",
          text:
            result.issues.length > 0
              ? `有 ${result.issues.length} 个问题，修复后才能上线。`
              : undefined,
          issues: result.issues,
        });
      }
      onListChangedRef.current?.();
      if (closeAfter) {
        route.close({ replace: true });
      } else if (isNew) {
        keepResultsForRef.current = newKey;
        route.openModel(newKey, { replace: true });
      }
      return { key: newKey, issues: result.issues };
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  /** dry-run / 试跑都基于服务端已保存的配置，有未保存改动先存；返回可用的模型 key */
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

  /** 测试节点组装出的 input；有没填的必填项就标红并提示，不发请求 */
  const parseSample = (): { ok: true; value: unknown } | { ok: false } => {
    setShowTestErrors(true);
    const count = Object.keys(testInput.errors).length;
    if (count === 0) return { ok: true, value: testInput.input };
    pushEntry({
      title: `测试节点还有 ${count} 项没填好`,
      tone: "error",
      text: Object.values(testInput.errors)[0],
    });
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
        setDryRun({ time: Date.now(), error: "插件运行器暂时不可用，稍后重试。测试参数已保留。" });
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

  const toggleEnabled = async (enabled: boolean) => {
    if (!key) return;
    setBusy("enabled");
    try {
      await setModelEnabled(key, enabled);
      if (!aliveRef.current) return;
      pushEntry({ title: `${key} 已${enabled ? "上线" : "下线"}`, tone: "success" });
      onListChangedRef.current?.();
      setDetail((prev) => (prev ? { ...prev, enabled } : prev));
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  return {
    // 选中
    selection,
    key,
    isNew,
    modelKey,
    // 列表
    // 编辑器
    clearEditor,
    detail,
    loadingDetail,
    text,
    setText,
    dirty,
    parsed,
    body,
    capabilities,
    addTestRef,
    removeTestRef,
    testParams,
    testAssets,
    testInput,
    showTestErrors,
    setTestParam,
    mode,
    modeError,
    switchMode,
    epoch,
    issues,
    checks,
    blockers,
    fieldIssues,
    fieldWarnings,
    editBody,
    format,
    // 状态
    busy,
    working,
    info,
    // 动作
    selectModel,
    closeEditor,
    startNew,
    save,
    validate,
    doDryRun,
    doTestRun,
    toggleEnabled,
    refreshTrace,
    // 结果
    dryRun,
    run,
    trace,
    resultTab,
    setResultTab,
    clearResults,
    pushEntry,
    // 对话框
    pendingNav,
    confirmNav,
    cancelNav: () => setPendingNav(null),
  };
}

/** 工作区对象的类型，供子组件的 props 使用 */
export type ModelWorkspace = ReturnType<typeof useModelWorkspace>;
