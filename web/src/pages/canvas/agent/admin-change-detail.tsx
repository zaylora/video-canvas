import { useState } from "react";
import { ChevronDown, ChevronRight, CircleAlert, Lock, TriangleAlert } from "lucide-react";

import type { AdminRole } from "@/api/admin/ai/type";
import type { AdminChangeItem, AdminChangePayload } from "@/api/agent/type";
import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";
import {
  basisRows,
  formatDiffValue,
  isPickable,
  resolveSelection,
} from "@/utils/admin/assistant/change-order";

import { MoreButton, useFold } from "./fold-list";

/** 待审核时条目自己的状态：点的颜色 + 文字 */
const ITEM_STATUS: Record<AdminChangeItem["status"], { dot: string; label: string }> = {
  ok: { dot: "bg-status-success", label: "可应用" },
  invalid: { dot: "bg-destructive", label: "校验未通过" },
  stale: { dot: "bg-status-warning", label: "已过期" },
  mismatch: { dot: "bg-destructive", label: "价格不符" },
};

/** 一条提示行：校验问题（bad）或警告（warn） */
function Note({ tone, children }: { tone: "bad" | "warn"; children: string }) {
  const Icon = tone === "bad" ? CircleAlert : TriangleAlert;
  return (
    <div
      className={cn(
        "ml-6 flex gap-1.5 rounded-md px-2 py-1 text-[11.5px] leading-relaxed",
        tone === "bad"
          ? "bg-destructive/8 text-destructive"
          : "bg-status-warning/10 text-status-warning",
      )}
    >
      <Icon aria-hidden className="mt-0.5 size-3.5 shrink-0" />
      <span className="min-w-0 wrap-anywhere">{children}</span>
    </div>
  );
}

/** 一项的字段差异：字段 / 当前（删除线）/ → / 变更后（绿色），数字等宽对齐 */
function DiffRows({ item }: { item: AdminChangeItem }) {
  return (
    <div
      aria-label="当前到变更后"
      className="ml-6 grid grid-cols-[minmax(0,1fr)_auto_14px_auto] items-baseline gap-x-2 gap-y-0.5 font-mono text-xs tabular-nums"
    >
      {item.diff.map((d) => {
        const before = formatDiffValue(d.before);
        const after = formatDiffValue(d.after);
        return (
          <div key={d.path} className="col-span-4 grid grid-cols-subgrid items-baseline">
            <span className="text-muted-foreground truncate" title={d.path}>
              {d.path}
            </span>
            <span
              title={before}
              className={cn(
                "text-muted-foreground max-w-32 truncate text-right",
                d.before !== null && d.before !== undefined && "text-destructive line-through",
              )}
            >
              {before}
            </span>
            <span aria-hidden className="text-muted-foreground text-center">
              →
            </span>
            <span
              title={after}
              className="text-status-success max-w-32 truncate text-right font-medium"
            >
              {after}
            </span>
          </div>
        );
      })}
    </div>
  );
}

/** 插件、渠道和新建模型的摘要；代码差异默认折叠，配色沿用决定区语义 token */
function CreationDetail({ item }: { item: AdminChangeItem }) {
  const [codeOpen, setCodeOpen] = useState(false);
  const detail = item.detail;
  if (!detail) return null;
  if (item.target === "plugin" && "plugin_key" in detail) {
    return (
      <div className="ml-6 flex flex-col gap-1 text-xs wrap-anywhere">
        <p className="font-mono">
          {detail.version_before === null
            ? `新插件 · ${detail.code_lines} 行`
            : `${detail.version_before} → ${detail.version_after}`}
        </p>
        <p className="text-muted-foreground">
          {[detail.meta.name, detail.meta.vendor, detail.meta.auth, detail.meta.endpoints]
            .filter(Boolean)
            .join(" · ")}
        </p>
        <p className={detail.check.ok ? "text-status-success" : "text-destructive"}>
          {detail.check.ok ? "静态验证通过" : "静态验证未通过"}
        </p>
        {detail.check.request && (
          <p className="font-mono">
            {detail.check.request.method} {detail.check.request.url}
          </p>
        )}
        {detail.check.issues.map((issue) => (
          <p key={issue} className="text-destructive">
            {issue}
          </p>
        ))}
        {detail.code_diff && (
          <div>
            <button
              type="button"
              aria-expanded={codeOpen}
              onClick={() => setCodeOpen(!codeOpen)}
              className="text-muted-foreground hover:text-foreground focus-visible:ring-node-ring/60 inline-flex min-h-7 items-center gap-1 rounded px-1 outline-none focus-visible:ring-2"
            >
              {codeOpen ? (
                <ChevronDown aria-hidden className="size-3.5" />
              ) : (
                <ChevronRight aria-hidden className="size-3.5" />
              )}
              代码差异
            </button>
            {codeOpen && (
              <pre
                aria-label="插件代码差异"
                className="bg-foreground/5 max-h-48 overflow-auto rounded-md p-2 font-mono text-[11px] leading-5"
              >
                {detail.code_diff.split("\n").map((line, index) => (
                  <span
                    key={index}
                    className={cn(
                      "block",
                      line.startsWith("+") && "bg-status-success/10 text-status-success",
                      line.startsWith("-") && "bg-destructive/10 text-destructive",
                    )}
                  >
                    {line || " "}
                  </span>
                ))}
              </pre>
            )}
          </div>
        )}
      </div>
    );
  }
  if (item.target === "channel" && "base_url" in detail) {
    return (
      <div className="text-muted-foreground ml-6 flex flex-col gap-1 text-xs wrap-anywhere">
        <p className="font-mono">{detail.base_url}</p>
        <p>请求头：{detail.headers.join("、") || "—"}</p>
        <p>{detail.secret === "configured" ? "已配置密钥" : "未配置密钥"}</p>
      </div>
    );
  }
  if (item.target === "model_create" && "upstream_model" in detail) {
    return (
      <dl className="ml-6 grid grid-cols-[auto_minmax(0,1fr)] gap-x-2 gap-y-1 text-xs wrap-anywhere">
        {[
          ["种类", detail.kind],
          ["渠道", detail.channel],
          ["上游模型", detail.upstream_model],
          ["插件", detail.plugin],
        ].map(([label, value]) => (
          <div key={label} className="col-span-2 grid grid-cols-subgrid">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="font-mono">{value}</dd>
          </div>
        ))}
      </dl>
    );
  }
  return null;
}

/** 一项的「计算依据」：默认收起，展开后是键值表，来源链接可点 */
function Basis({ item }: { item: AdminChangeItem }) {
  const [open, setOpen] = useState(false);
  if (item.basis.length === 0) return null;
  return (
    <div className="ml-6">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className="text-muted-foreground hover:text-foreground focus-visible:ring-node-ring/60 -ml-1 inline-flex h-6 items-center gap-0.5 rounded px-1 text-[11px] outline-none focus-visible:ring-2"
      >
        {open ? (
          <ChevronDown aria-hidden className="size-3.5" />
        ) : (
          <ChevronRight aria-hidden className="size-3.5" />
        )}
        计算依据
      </button>
      {open &&
        item.basis.map((basis) => (
          <dl
            key={basis.path}
            className="bg-foreground/5 mt-1 grid grid-cols-[72px_minmax(0,1fr)] gap-x-2.5 gap-y-1 rounded-md px-2.5 py-2 font-mono text-[11px]"
          >
            <dt className="text-muted-foreground col-span-2 break-all">{basis.path}</dt>
            {basisRows(basis).map((row) => (
              <div key={row.label} className="col-span-2 grid grid-cols-subgrid">
                <dt className="text-muted-foreground">{row.label}</dt>
                <dd className="m-0 break-all">
                  {row.href ? (
                    <a
                      href={row.href}
                      target="_blank"
                      rel="noreferrer noopener"
                      className="underline underline-offset-2"
                    >
                      {row.value}
                    </a>
                  ) : (
                    row.value
                  )}
                </dd>
              </div>
            ))}
          </dl>
        ))}
    </div>
  );
}

/**
 * 变更单审批的明细（消息流里的审批卡）：单号和条数；每一项有勾选框、名称、key、状态，下面是「当前 → 变更后」、
 * 校验问题、警告和可展开的计算依据。超过 3 项时只露前 3 项，其余收进「还有 N 项」，折叠不影响勾选。有问题的条目、角色不够的条目照样展示（让人看到助手哪里没做对），但不能勾选。
 * @param payload 变更单内容
 * @param role 当前管理员角色，决定哪些条目能勾选
 * @param picked 勾选的条目下标
 * @param disabled 正在提交，勾选框锁定
 * @param onToggle 切换一项的勾选
 */
export function AdminChangeDetail({
  payload,
  role,
  picked,
  disabled,
  onToggle,
}: {
  payload: AdminChangePayload;
  role: AdminRole | null;
  picked: ReadonlySet<number>;
  disabled: boolean;
  onToggle: (index: number) => void;
}) {
  const liveCount = payload.items.filter((i) => i.live).length;
  const selection = resolveSelection(payload.items, role, picked);
  const fold = useFold(payload.items.length);
  return (
    <div className="flex flex-col gap-1.5">
      <p className="text-muted-foreground text-[11.5px]">
        {payload.order_no && <span className="font-mono">{payload.order_no} · </span>}
        {payload.items.length} 项{liveCount > 0 && ` · ${liveCount} 项应用即生效`}
        {payload.revert_of && ` · 回滚 ${payload.revert_of}`}
      </p>
      <ul className="flex flex-col gap-2.5">
        {payload.items.slice(0, fold.visible).map((item, index) => {
          const allowed = isPickable(item, role);
          const pickable = allowed && (item.depends_on ?? []).every((i) => selection.has(i));
          const roleShort = item.status === "ok" && !allowed;
          const tag = ITEM_STATUS[item.status];
          return (
            <li key={`${item.target}:${item.key}:${index}`} className="flex flex-col gap-1">
              <div className="flex items-center gap-2 text-xs">
                <Checkbox
                  aria-label={`选择 ${item.label}`}
                  checked={selection.has(index)}
                  disabled={!pickable || disabled}
                  onCheckedChange={() => onToggle(index)}
                />
                <span className={cn("font-medium wrap-anywhere", !pickable && "opacity-55")}>
                  {item.label}
                </span>
                <span className="text-muted-foreground min-w-0 flex-1 truncate font-mono text-[11px]">
                  {item.key}
                </span>
                {roleShort ? (
                  <span className="text-muted-foreground inline-flex shrink-0 items-center gap-1 text-[11px] whitespace-nowrap">
                    <Lock aria-hidden className="size-3" />需 {item.required_role}
                  </span>
                ) : (
                  <span className="text-muted-foreground inline-flex shrink-0 items-center gap-1.5 text-[11px] whitespace-nowrap">
                    <i aria-hidden className={cn("size-1.5 rounded-full", tag.dot)} />
                    {tag.label}
                  </span>
                )}
              </div>
              <div className={cn(!pickable && "opacity-55")}>
                <CreationDetail item={item} />
                <DiffRows item={item} />
              </div>
              {(item.depends_on?.length ?? 0) > 0 && (
                <p className="text-muted-foreground ml-6 text-[11.5px]">
                  {item
                    .depends_on!.map(
                      (i) => `依赖第 ${i + 1} 项（${payload.items[i]?.label ?? "不存在"}）`,
                    )
                    .join(" · ")}
                </p>
              )}
              {item.issues.map((text) => (
                <Note key={text} tone="bad">
                  {text}
                </Note>
              ))}
              {item.warnings.map((text) => (
                <Note key={text} tone="warn">
                  {text}
                </Note>
              ))}
              <Basis item={item} />
            </li>
          );
        })}
      </ul>
      <MoreButton count={fold.hidden} onClick={fold.expand} />
    </div>
  );
}
