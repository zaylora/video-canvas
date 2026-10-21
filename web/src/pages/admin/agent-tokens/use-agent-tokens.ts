import { useCallback, useEffect, useReducer, useState } from "react";
import { toast } from "sonner";

import { issueAgentToken, listAgentTokens, revokeAgentToken } from "@/api/admin/agent-token";
import type { AgentTokenItem } from "@/api/admin/agent-token/type.d";
import {
  TOKEN_DAYS_DEFAULT,
  initialTokenPage,
  runIssue,
  tokenPageReducer,
  type IssueFormValues,
} from "@/utils/admin/agent-token";

import { useAliveRef } from "../use-admin";

const EMPTY_FORM: IssueFormValues = { name: "", days: String(TOKEN_DAYS_DEFAULT) };

/**
 * 分析令牌页的状态和操作：拉列表、签发（先本地校验）、「我已保存」关掉明文、注销。
 * 请求错误的全局提示由拦截器弹，这里只管状态：签发失败时表单内容保留，方便直接重试。
 * @returns 页面状态、表单值与错误、各种操作
 */
export function useAgentTokens() {
  const aliveRef = useAliveRef();
  const [state, dispatch] = useReducer(tokenPageReducer, initialTokenPage);
  const [values, setValues] = useState<IssueFormValues>(EMPTY_FORM);
  const [errors, setErrors] = useState<{ name?: string; days?: string }>({});
  const [issuing, setIssuing] = useState(false);

  const reload = useCallback(async () => {
    try {
      const items = await listAgentTokens();
      if (aliveRef.current) dispatch({ type: "loaded", items });
    } catch {
      if (aliveRef.current) dispatch({ type: "load-failed" });
    }
  }, [aliveRef]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const change = useCallback((next: IssueFormValues) => {
    setValues(next);
    setErrors({});
  }, []);

  const issue = useCallback(async () => {
    if (issuing) return;
    setIssuing(true);
    try {
      const result = await runIssue(values, issueAgentToken);
      if (!aliveRef.current) return;
      if (!result.ok) {
        setErrors(result.errors);
        return;
      }
      setErrors({});
      setValues(EMPTY_FORM);
      dispatch({ type: "issued", result: result.result });
      toast.success("已签发，写入操作审计");
    } catch {
      // 全局提示已经弹了，表单内容保留
    } finally {
      if (aliveRef.current) setIssuing(false);
    }
  }, [aliveRef, issuing, values]);

  const dismiss = useCallback(() => dispatch({ type: "dismissed" }), []);

  /** 注销：成功后该行立即从列表消失；失败时抛出，由确认框把原因留在框里 */
  const revoke = useCallback(
    async (item: AgentTokenItem) => {
      await revokeAgentToken(item.id);
      if (!aliveRef.current) return;
      dispatch({ type: "revoked", id: item.id, at: new Date().toISOString() });
      toast.success(`已注销「${item.name}」，写入操作审计`);
    },
    [aliveRef],
  );

  return { state, values, errors, issuing, change, issue, dismiss, revoke, reload };
}
