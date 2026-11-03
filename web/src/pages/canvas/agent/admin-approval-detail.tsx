import { useState } from "react";

import { setChannelSecret } from "@/api/admin/ai";
import type { AdminRole } from "@/api/admin/ai/type";
import type { AdminSecretPayload, AdminTestRunPayload } from "@/api/agent/type";
import type { AgentController } from "@/hooks/use-agent-controller";
import { roleSatisfies } from "@/utils/admin/assistant/change-order";

/** 密钥仅留在本输入框的组件状态中，直达渠道接口，决定请求只报告已配置 */
export function AdminSecretDetail({
  payload,
  approvalId,
  role,
  ctl,
}: {
  payload: AdminSecretPayload;
  approvalId: string;
  role: AdminRole | null;
  ctl: AgentController;
}) {
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const blocked = !payload.exists
    ? "先应用建渠道的变更单"
    : !roleSatisfies(payload.required_role, role)
      ? "需 super_admin 才能保存密钥"
      : "";

  const submit = async (save: boolean) => {
    if (busy || (save && (blocked || !secret.trim()))) return;
    setBusy(true);
    setError("");
    let saved = false;
    try {
      if (save) {
        const request = setChannelSecret(payload.channel_key, secret);
        setSecret("");
        await request;
        saved = true;
      } else {
        setSecret("");
      }
      const outcome = await ctl.decide(
        approvalId,
        save ? { decision: "approve", answer: "configured" } : { decision: "reject" },
      );
      if (outcome === "failed") setError(save ? "密钥已保存，确认失败" : "跳过失败");
    } catch {
      /** 请求错误由全局拦截器提示；就地反馈只用固定文案，避免异常携带密钥 */
      setError(saved ? "密钥已保存，确认失败" : save ? "密钥保存失败" : "跳过失败");
    } finally {
      setSecret("");
      setBusy(false);
    }
  };

  const actionClass =
    "hover:bg-chrome-hover focus-visible:ring-node-ring/60 min-h-10 rounded-lg px-3 text-left text-[13px] outline-none focus-visible:ring-2 disabled:opacity-45";
  return (
    <form
      className="flex flex-col gap-2"
      onSubmit={(event) => {
        event.preventDefault();
        void submit(true);
      }}
    >
      <p className="text-muted-foreground text-xs">
        {payload.secret_configured ? "已配置密钥" : "未配置密钥"}
      </p>
      <input
        type="password"
        autoComplete="off"
        aria-label="渠道 API Key"
        value={secret}
        disabled={busy || !!blocked}
        onChange={(event) => setSecret(event.target.value)}
        className="bg-chrome-hover border-chrome-border focus-visible:ring-node-ring/60 h-10 min-w-0 rounded-full border px-3 text-[13px] outline-none focus-visible:ring-2 disabled:opacity-45"
      />
      {blocked && <p className="text-muted-foreground text-xs">{blocked}</p>}
      {busy && (
        <p role="status" className="text-muted-foreground text-xs">
          保存中…
        </p>
      )}
      {error && (
        <p role="alert" className="text-destructive text-xs">
          {error}
        </p>
      )}
      <div className="flex gap-2">
        <button
          type="submit"
          disabled={busy || !!blocked || !secret.trim()}
          className={actionClass}
        >
          保存
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={() => void submit(false)}
          className={actionClass}
        >
          跳过
        </button>
      </div>
    </form>
  );
}

/** 试跑输入只展示摘要，估价仅作示意，不从管理员余额扣费 */
export function AdminTestRunDetail({ payload }: { payload: AdminTestRunPayload }) {
  const estimate = [
    payload.estimate.credits !== null ? `${payload.estimate.credits} 积分` : null,
    payload.estimate.yuan !== null ? `¥${payload.estimate.yuan}` : null,
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <div className="text-muted-foreground flex flex-col gap-1 text-xs wrap-anywhere">
      <p>{payload.input_summary}</p>
      <p>预估费用（示意）：{estimate || "暂无法估算"}</p>
      <p>
        第 {payload.attempt} 次 / 共 3 次 · 剩余 {payload.attempts_left} 次
      </p>
    </div>
  );
}
