import { useCallback, useEffect, useRef, useState } from "react";

import { getUsageDetail } from "@/api/admin/usage";
import type { UsageDetail } from "@/api/admin/usage/type";
import { isTaskNotFound, shouldPoll, TASK_POLL_MS } from "@/utils/admin/log-rules";

export type DetailPhase = "loading" | "ready" | "error" | "notfound";

/** 页面当前是否可见；切到别的标签页就暂停轮询 */
function usePageVisible() {
  const [visible, setVisible] = useState(() => !document.hidden);
  useEffect(() => {
    const onChange = () => setVisible(!document.hidden);
    document.addEventListener("visibilitychange", onChange);
    return () => document.removeEventListener("visibilitychange", onChange);
  }, []);
  return visible;
}

/**
 * 任务详情：打开抽屉时拉一次；任务还在进行中就每 5 秒静默刷新，直到终态，页面不可见时暂停。
 * 换任务或重试会丢弃旧请求的结果；静默刷新失败不改变已显示的内容（全局 toast 已弹）。
 * @param ref 任务编号；null 表示抽屉没有打开
 */
export function useTaskDetail(ref: string | null) {
  const [phase, setPhase] = useState<DetailPhase>("loading");
  const [detail, setDetail] = useState<UsageDetail | null>(null);
  const [attempt, setAttempt] = useState(0);
  const seq = useRef(0);
  const visible = usePageVisible();

  useEffect(() => {
    const mine = ++seq.current;
    if (!ref) return;
    setPhase("loading");
    setDetail(null);
    getUsageDetail(ref)
      .then((next) => {
        if (mine !== seq.current) return;
        setDetail(next);
        setPhase("ready");
      })
      .catch((error: unknown) => {
        if (mine === seq.current) setPhase(isTaskNotFound(error) ? "notfound" : "error");
      });
  }, [ref, attempt]);

  const status = detail?.task.status;
  const polling = !!ref && !!status && shouldPoll(status, visible);
  useEffect(() => {
    if (!ref || !polling) return;
    const timer = setInterval(() => {
      const mine = seq.current;
      getUsageDetail(ref)
        .then((next) => {
          if (mine === seq.current) setDetail(next);
        })
        .catch(() => undefined);
    }, TASK_POLL_MS);
    return () => clearInterval(timer);
  }, [ref, polling]);

  const retry = useCallback(() => setAttempt((n) => n + 1), []);
  return {
    phase,
    detail,
    retry,
    /** 正在自动刷新 */
    polling,
    /** 进行中但页面不可见，刷新已暂停 */
    paused: !!status && !visible && shouldPoll(status, true),
  };
}
