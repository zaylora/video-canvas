import { useState } from "react";
import { Check, Hand, ShieldAlert, Sparkles, Trash2, X } from "lucide-react";

import type {
  AgentApprovalDto,
  AgentAskPayload,
  AgentDeletePayload,
  AgentGeneratePayload,
} from "@/api/agent/type";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import type { AgentController } from "@/hooks/use-agent-controller";
import { cn } from "@/lib/utils";

import { Card } from "./message-list";

/** 审批状态给用户看的话 */
const STATUS_TEXT: Record<string, string> = {
  approved: "已批准",
  partially_approved: "已部分批准",
  rejected: "已拒绝",
  expired: "已失效",
  executed: "已批准并执行",
  failed: "已批准，但执行失败",
};

/**
 * 生成、删除、提问三种卡片：内容取审批的最新状态，所以批准后卡片自己变成「已批准」。
 * 待处理时卡片高亮，等你的决定；已处理或失效后只读。
 */
export function ApprovalCard({ approvalId, ctl }: { approvalId: string; ctl: AgentController }) {
  const approval = ctl.state.approvals[approvalId];
  if (!approval) return null;
  switch (approval.kind) {
    case "generate":
      return <GenerateCard approval={approval} ctl={ctl} />;
    case "delete":
      return <DeleteCard approval={approval} ctl={ctl} />;
    case "ask":
      return <AskCard approval={approval} ctl={ctl} />;
  }
}

/** 已决定后的一行状态 */
function Settled({ approval }: { approval: AgentApprovalDto }) {
  return (
    <p className="text-muted-foreground flex items-center gap-1.5 text-xs">
      {approval.status === "rejected" || approval.status === "expired" ? (
        <X className="size-3.5" />
      ) : (
        <Check className="size-3.5" />
      )}
      {STATUS_TEXT[approval.status] ?? approval.status}
    </p>
  );
}

function GenerateCard({ approval, ctl }: { approval: AgentApprovalDto; ctl: AgentController }) {
  const payload = approval.payload as AgentGeneratePayload;
  const pending = approval.status === "pending";
  /** 每一项勾没勾、生成几个（只能不多于申请的） */
  const [picks, setPicks] = useState(() =>
    payload.items.map((it) => ({ on: true, count: it.count })),
  );
  const [busy, setBusy] = useState(false);
  const [overBudget, setOverBudget] = useState(false);
  const quote = payload.items.reduce(
    (sum, it, i) => sum + (picks[i]?.on ? it.price * (picks[i]?.count ?? 0) : 0),
    0,
  );
  const chosen = picks.filter((p) => p.on).length;

  const decide = async (approve: boolean, addBudget = 0) => {
    setBusy(true);
    const result = await ctl.decide(approval.id, {
      decision: approve ? "approve" : "reject",
      ...(approve
        ? {
            items: picks.map((p, index) => ({ index, approve: p.on, count: p.count })),
            add_budget: addBudget,
          }
        : {}),
    });
    setBusy(false);
    if (result === "over_budget") setOverBudget(true);
  };

  return (
    <Card tone="neutral" icon={<Sparkles className="text-status-warning size-4" />}>
      <div
        className={cn(
          "-m-3 flex flex-col gap-2 rounded-xl border p-3",
          pending ? "border-status-warning/60" : "border-transparent",
        )}
      >
        <p className="font-medium">申请生成 {payload.items.length} 个节点</p>
        {payload.reason && <p className="text-muted-foreground">{payload.reason}</p>}
        <ul className="flex flex-col gap-1">
          {payload.items.map((it, i) => (
            <li key={it.node_id} className="flex items-center gap-2 text-xs">
              {pending && (
                <Checkbox
                  checked={picks[i].on}
                  onCheckedChange={(on) =>
                    setPicks((prev) =>
                      prev.map((p, j) => (j === i ? { ...p, on: on === true } : p)),
                    )
                  }
                  aria-label={`生成「${it.label}」`}
                />
              )}
              <span className="min-w-0 flex-1 truncate">{it.label}</span>
              <span className="text-muted-foreground shrink-0">{it.model_name}</span>
              <span className="text-credit shrink-0 tabular-nums">✦ {it.price * it.count}</span>
            </li>
          ))}
        </ul>
        {pending ? (
          <>
            <div className="flex items-center justify-between text-xs">
              <span className="text-muted-foreground">预估合计</span>
              <span className="text-credit font-medium tabular-nums">✦ {quote}</span>
            </div>
            {overBudget && (
              <p className="text-destructive flex items-center gap-1 text-xs">
                <ShieldAlert className="size-3.5" /> 超出本轮积分预算，可以追加预算后批准
              </p>
            )}
            <div className="flex flex-wrap justify-end gap-1.5">
              <Button size="sm" variant="ghost" disabled={busy} onClick={() => void decide(false)}>
                拒绝
              </Button>
              {overBudget && (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={busy || chosen === 0}
                  onClick={() => void decide(true, quote)}
                >
                  追加 {quote} 预算并批准
                </Button>
              )}
              <Button size="sm" disabled={busy || chosen === 0} onClick={() => void decide(true)}>
                批准生成
              </Button>
            </div>
          </>
        ) : (
          <Settled approval={approval} />
        )}
      </div>
    </Card>
  );
}

function DeleteCard({ approval, ctl }: { approval: AgentApprovalDto; ctl: AgentController }) {
  const payload = approval.payload as AgentDeletePayload;
  const pending = approval.status === "pending";
  const [keep, setKeep] = useState(() => payload.node_ids.map(() => true));
  const [busy, setBusy] = useState(false);
  const count = keep.filter(Boolean).length + payload.edge_ids.length;

  const decide = async (approve: boolean) => {
    setBusy(true);
    await ctl.decide(approval.id, {
      decision: approve ? "approve" : "reject",
      ...(approve ? { items: keep.map((on, index) => ({ index, approve: on })) } : {}),
    });
    setBusy(false);
  };

  return (
    <Card tone="neutral" icon={<Trash2 className="text-destructive size-4" />}>
      <div
        className={cn(
          "-m-3 flex flex-col gap-2 rounded-xl border p-3",
          pending ? "border-destructive/60" : "border-transparent",
        )}
      >
        <p className="font-medium">
          申请删除 {payload.node_ids.length} 个节点
          {payload.edge_ids.length > 0 && `、${payload.edge_ids.length} 条连线`}
        </p>
        {payload.reason && <p className="text-muted-foreground">{payload.reason}</p>}
        <ul className="flex flex-col gap-1">
          {payload.node_ids.map((id, i) => (
            <li key={id} className="flex items-center gap-2 text-xs">
              {pending && (
                <Checkbox
                  checked={keep[i]}
                  onCheckedChange={(on) =>
                    setKeep((prev) => prev.map((v, j) => (j === i ? on === true : v)))
                  }
                  aria-label={`删除「${payload.labels[i] ?? id}」`}
                />
              )}
              <span className="min-w-0 flex-1 truncate">{payload.labels[i] ?? id}</span>
              {payload.outputs[i] && <span className="text-destructive shrink-0">含产物</span>}
            </li>
          ))}
        </ul>
        {pending ? (
          <div className="flex justify-end gap-1.5">
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => void decide(false)}>
              保留
            </Button>
            <Button
              size="sm"
              variant="destructive"
              disabled={busy || count === 0}
              onClick={() => void decide(true)}
            >
              删除 {count} 项
            </Button>
          </div>
        ) : (
          <Settled approval={approval} />
        )}
      </div>
    </Card>
  );
}

function AskCard({ approval, ctl }: { approval: AgentApprovalDto; ctl: AgentController }) {
  const payload = approval.payload as AgentAskPayload;
  const pending = approval.status === "pending";
  const [custom, setCustom] = useState("");
  const [model, setModel] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const answered = typeof approval.decision?.answer === "string" ? approval.decision.answer : "";

  const answer = async (text: string) => {
    if (!text.trim()) return;
    setBusy(true);
    await ctl.decide(approval.id, { decision: "approve", answer: text.trim() });
    setBusy(false);
  };

  return (
    <Card tone="info" icon={<Hand className="text-status-running size-4" />}>
      <div className="flex flex-col gap-2">
        <p className="font-medium">{payload.question}</p>
        {!pending ? (
          answered ? (
            <p className="text-muted-foreground text-xs">你的回答：{answered}</p>
          ) : (
            <Settled approval={approval} />
          )
        ) : payload.kind === "model" ? (
          <>
            <ul className="flex flex-col gap-1" role="radiogroup">
              {(payload.models ?? []).map((m) => (
                <li key={m.key}>
                  <button
                    type="button"
                    role="radio"
                    aria-checked={model === m.key}
                    onClick={() => setModel(m.key)}
                    className={cn(
                      "ring-chrome-border hover:bg-chrome-hover flex w-full flex-col rounded-lg px-2.5 py-1.5 text-left text-xs ring-1 outline-none",
                      "focus-visible:ring-node-ring/60 focus-visible:ring-2",
                      model === m.key && "bg-foreground/8 ring-foreground/30",
                    )}
                  >
                    <span className="font-medium">{m.name}</span>
                    {m.hint && <span className="text-muted-foreground truncate">{m.hint}</span>}
                  </button>
                </li>
              ))}
            </ul>
            <div className="flex justify-end">
              <Button
                size="sm"
                disabled={busy || !model}
                onClick={() => {
                  const m = payload.models?.find((x) => x.key === model);
                  if (m) void answer(`${m.name}（模型 key：${m.key}）`);
                }}
              >
                用这个
              </Button>
            </div>
          </>
        ) : (
          <>
            <div className="flex flex-wrap gap-1.5">
              {(payload.options ?? []).map((o) => (
                <Button
                  key={o}
                  size="sm"
                  variant="outline"
                  disabled={busy}
                  onClick={() => void answer(o)}
                >
                  {o}
                </Button>
              ))}
            </div>
            {payload.allow_custom && (
              <form
                className="flex gap-1.5"
                onSubmit={(e) => {
                  e.preventDefault();
                  void answer(custom);
                }}
              >
                <Input
                  value={custom}
                  onChange={(e) => setCustom(e.target.value)}
                  placeholder="或者自己写"
                  className="h-8 text-xs"
                  maxLength={2000}
                />
                <Button size="sm" type="submit" disabled={busy || !custom.trim()}>
                  回答
                </Button>
              </form>
            )}
          </>
        )}
      </div>
    </Card>
  );
}
