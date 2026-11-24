import { useState } from "react";
import { Braces, Cable, KeyRound, Plus } from "lucide-react";
import { toast } from "sonner";

import type { MCPProjectItem, MCPTokenItem } from "@/api/admin/mcp-project/type";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import {
  CopyBlock,
  CopyBlockCode,
  CopyBlockHeader,
  CopyBlockTitle,
} from "@/components/admin-ui/copy-block";
import { CopyButton } from "@/components/admin-ui/copy-button";
import { DataPage, DataPanel, DataPanelBody } from "@/components/admin-ui/data-panel";
import { EmptyState, EmptyStateIcon, EmptyStateTitle } from "@/components/admin-ui/empty-state";
import { Notice } from "@/components/admin-ui/notice";
import {
  PageHeader,
  PageHeaderActions,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { RowAction } from "@/components/admin-ui/row-action";
import {
  SecretReveal,
  SecretRevealActions,
  SecretRevealCode,
  SecretRevealHint,
  SecretRevealTitle,
  SecretRevealValue,
} from "@/components/admin-ui/secret-reveal";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  TOKEN_STATUS_LABEL,
  formatMCPTime,
  mcpConfigSnippet,
  scopeLabel,
  tokenStatus,
} from "@/utils/admin/mcp-project";
import { useNow } from "@/hooks/use-now";
import { copyToClipboard } from "@/utils/admin/clipboard";

import { ProjectDialog, type ProjectDialogTarget } from "./project-dialog";
import type { MCPProjectsAudience, MCPProjectsState } from "./use-mcp-projects";

const PANEL =
  "bg-chrome ring-chrome-border mb-4 rounded-3xl p-4 shadow-lg ring-1 backdrop-blur-xl backdrop-saturate-150";

/** 项目表格。 */
function ProjectTable({
  state,
  mode,
  onEdit,
}: {
  state: MCPProjectsState;
  mode: MCPProjectsAudience;
  onEdit: (item: MCPProjectItem) => void;
}) {
  if (state.items.length === 0) {
    return (
      <EmptyState className="m-4">
        <EmptyStateIcon>
          <Cable />
        </EmptyStateIcon>
        <EmptyStateTitle>还没有接入项目</EmptyStateTitle>
      </EmptyState>
    );
  }
  return (
    <Table>
      <TableHeader>
        <TableRow className="bg-muted/40 hover:bg-muted/40">
          <TableHead className="px-4">名称</TableHead>
          <TableHead>能力</TableHead>
          <TableHead>最近使用</TableHead>
          <TableHead>状态</TableHead>
          <TableHead className="w-40 px-4 text-right">操作</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {state.items.map((item) => (
          <TableRow key={item.id} data-project-id={item.id}>
            {/* 归属用户与默认画布是「一个项目就一个」的配置项，各占一列等于重复铺排，
                压成名称下面的一行副标题，主列留给真正会扫的信息（能力 / 令牌 / 使用 / 状态） */}
            <TableCell className="px-4 py-3">
              <div className="font-medium">{item.name}</div>
              <div className="text-muted-foreground text-xs">
                {mode === "admin" && <>{item.owner_name || `#${item.owner_user_id}`} · </>}
                {item.canvas_code ? (
                  <span title={item.canvas_code}>
                    默认画布「{item.canvas_title || `${item.canvas_code.slice(0, 8)}…`}」
                  </span>
                ) : (
                  "每次调用指定画布"
                )}
              </div>
            </TableCell>
            <TableCell className="py-3">
              <div className="flex flex-wrap gap-1">
                {item.scopes.length === 0 ? (
                  <span className="text-muted-foreground text-xs">未开放</span>
                ) : (
                  item.scopes.map((s) => (
                    <Tag key={s} tone="neutral">
                      {scopeLabel(s)}
                    </Tag>
                  ))
                )}
              </div>
            </TableCell>
            <TableCell className="text-muted-foreground py-3 tabular-nums">
              {formatMCPTime(item.last_used_at)}
            </TableCell>
            <TableCell className="py-3">
              <Tag tone={item.enabled ? "success" : "neutral"}>
                {item.enabled ? "启用中" : "已停用"}
              </Tag>
            </TableCell>
            <TableCell className="px-4 py-3 text-right whitespace-nowrap">
              <RowAction aria-label={`编辑 ${item.name}`} onClick={() => onEdit(item)}>
                编辑
              </RowAction>
              <RowAction
                aria-label={`${item.name} 的接入配置`}
                onClick={() => state.toggleTokens(item.id)}
              >
                配置
              </RowAction>
              <RowAction
                aria-label={item.enabled ? `停用 ${item.name}` : `启用 ${item.name}`}
                onClick={() => void state.setEnabled(item, !item.enabled)}
              >
                {item.enabled ? "停用" : "启用"}
              </RowAction>
              <RowAction
                tone="danger"
                aria-label={`删除 ${item.name}`}
                onClick={() =>
                  void confirm({
                    title: `删除「${item.name}」？`,
                    description: "令牌一并删除，外部工具立刻断开，不可恢复",
                    destructive: true,
                    confirmLabel: "删除",
                    onConfirm: () => state.remove(item),
                  })
                }
              >
                删除
              </RowAction>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

/** 令牌状态标签。 */
function TokenStatusTag({ token, now }: { token: MCPTokenItem; now: number }) {
  const status = tokenStatus(token, now);
  const tone = status === "active" ? "success" : status === "expired" ? "warning" : "neutral";
  return <Tag tone={tone}>{TOKEN_STATUS_LABEL[status]}</Tag>;
}

/**
 * 接入配置：一个项目一块，不再往下套一层「令牌列表」。
 *
 * 两块内容：令牌（明文可复制、可隐藏，用 SecretReveal 展示），以及把地址与令牌拼好的配置片段。
 * 片段是这两样的唯一出处，页面别处不再重复给地址，免得同一份信息出现两遍。
 * 明文刚新建或刚重新生成时来自响应，其余时候点「获取并复制」按需取回（库里存 AES-GCM 密文）。
 */
function AccessPanel({
  state,
  project,
  now,
}: {
  state: MCPProjectsState;
  project: MCPProjectItem;
  now: number;
}) {
  const token = project.token ?? null;
  const issuedHere =
    state.issued && state.issued.item.project_id === project.id ? state.issued : null;
  const [revealed, setRevealed] = useState<string | null>(null);
  const [hidden, setHidden] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [failed, setFailed] = useState(false);
  const origin = typeof window === "undefined" ? "" : window.location.origin;
  const endpointPath = issuedHere?.endpoint ?? "/api/v1/mcp";
  /** 明文只有两个来源：刚签发的那次响应，或者用户点了取回之后向服务端要回来。 */
  const plain = hidden ? null : (issuedHere?.token ?? revealed);
  const masked = token ? `${token.prefix}${"•".repeat(24)}` : "";
  /** 配置片段是地址与令牌的唯一出处，明文还没取回时用占位符顶上。 */
  const snippet = mcpConfigSnippet(plain ?? "<令牌>", endpointPath, origin);

  /** 取回明文：手上已经有就直接用，否则向服务端要一次。取不到返回 null。 */
  const fetchPlain = async (): Promise<string | null> => {
    if (plain) return plain;
    if (!token) return null;
    setFetching(true);
    setFailed(false);
    try {
      const secret = await state.tokenSecret(project.id);
      setRevealed(secret);
      setHidden(false);
      return secret;
    } catch {
      setFailed(true);
      return null;
    } finally {
      setFetching(false);
    }
  };

  /** 取回并直接写剪贴板：外部工具要的就是这一串，不用先显示再手选。 */
  const revealAndCopy = async () => {
    const secret = await fetchPlain();
    if (!secret) {
      toast.error(token ? "令牌明文取不回来，重新生成一枚即可" : "这个项目还没有令牌，先生成一枚");
      return;
    }
    if (!(await copyToClipboard(secret))) {
      toast.error("浏览器不允许写剪贴板，请手动选中令牌复制");
      return;
    }
    toast.success("令牌已复制");
  };

  /** 复制整段配置：先确保拿到明文，再拼一次片段，避免把占位符复制出去。 */
  const copySnippet = async () => {
    const secret = await fetchPlain();
    if (!secret) {
      toast.error(token ? "令牌明文取不回来，重新生成一枚即可" : "这个项目还没有令牌，先生成一枚");
      return;
    }
    if (!(await copyToClipboard(mcpConfigSnippet(secret, endpointPath, origin)))) {
      toast.error("浏览器不允许写剪贴板，请手动选中配置复制");
      return;
    }
    toast.success("配置已复制");
  };

  return (
    <section className={PANEL} data-slot="mcp-access-panel">
      <h2 className="mb-3 flex items-center gap-2 text-sm font-semibold">
        <KeyRound className="size-4" aria-hidden="true" />「{project.name}」的接入配置
      </h2>
      {!project.enabled && (
        <Notice tone="warning" className="mb-3" title="项目已停用">
          外部工具现在连不上，启用后立即恢复。
        </Notice>
      )}
      <div className="grid gap-2">
        {plain ? (
          <SecretReveal data-slot="mcp-token-reveal">
            <SecretRevealTitle>令牌</SecretRevealTitle>
            <SecretRevealValue>
              <SecretRevealCode>{plain}</SecretRevealCode>
              <CopyButton text={plain} variant="outline" />
            </SecretRevealValue>
            <SecretRevealHint>
              填在请求头 <code>Authorization: Bearer</code> 后面
              {token && (
                <>
                  {" "}
                  · 有效期到 {formatMCPTime(token.expires_at)} · 最近使用{" "}
                  {formatMCPTime(token.last_used_at)}
                </>
              )}
            </SecretRevealHint>
            <SecretRevealActions>
              <Button variant="ghost" size="sm" onClick={() => setHidden(true)}>
                隐藏
              </Button>
            </SecretRevealActions>
          </SecretReveal>
        ) : (
          <CopyBlock data-slot="mcp-token-row">
            <CopyBlockHeader>
              <CopyBlockTitle>
                <KeyRound aria-hidden="true" />
                API Key
              </CopyBlockTitle>
              <Button
                variant="outline"
                size="xs"
                disabled={state.busy || fetching || !token}
                onClick={() => void revealAndCopy()}
              >
                {fetching ? "取回中…" : "获取并复制"}
              </Button>
            </CopyBlockHeader>
            <code className="block font-mono text-xs break-all">
              {token ? masked : "还没有令牌"}
            </code>
            {(failed || token) && (
              <p className="text-muted-foreground text-[11px]">
                {failed
                  ? "取不回明文（早期令牌没存密文，或服务端主密钥变过），重新生成一枚即可"
                  : `有效期到 ${formatMCPTime(token?.expires_at)} · 最近使用 ${formatMCPTime(token?.last_used_at)}`}
              </p>
            )}
          </CopyBlock>
        )}
        <CopyBlock data-slot="mcp-config-row">
          <CopyBlockHeader>
            <CopyBlockTitle>
              <Braces aria-hidden="true" />
              配置
            </CopyBlockTitle>
            <Button
              variant="ghost"
              size="xs"
              disabled={state.busy || fetching || !token}
              onClick={() => void copySnippet()}
            >
              {plain ? "复制配置" : "获取并复制"}
            </Button>
          </CopyBlockHeader>
          <CopyBlockCode>{snippet}</CopyBlockCode>
        </CopyBlock>
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        {token ? <TokenStatusTag token={token} now={now} /> : null}
        <div className="ml-auto flex gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={state.busy}
            onClick={() =>
              void confirm({
                title: token ? "重新生成令牌？" : "生成令牌？",
                description: token
                  ? "现在这枚会立刻失效，正在用它的外部工具要换成新的才能连上"
                  : "给这个项目生成一枚令牌，生成后就能复制",
                destructive: !!token,
                confirmLabel: token ? "重新生成" : "生成",
                onConfirm: () => state.rotate(project.id),
              })
            }
          >
            <KeyRound />
            {token ? "重新生成令牌" : "生成令牌"}
          </Button>
        </div>
      </div>
    </section>
  );
}

/**
 * MCP 接入页：说明、新建项目、项目表格、选中项目的接入配置。
 * 一个项目就是一个接入点，建项目时令牌已经签好；配置片段在展开的那一块里，不再单独分层。
 *
 * @param mode admin = 后台代管（能指定归属用户、看全部项目）；self = 用户自己的接入
 */
export function MCPProjectsView({
  state,
  mode = "admin",
}: {
  state: MCPProjectsState;
  mode?: MCPProjectsAudience;
}) {
  const now = useNow(true, 60_000);
  const selected = state.items.find((item) => item.id === state.selectedId) ?? null;
  /** 新建 / 编辑共用一个弹窗，和后台其他配置页一致。 */
  const [target, setTarget] = useState<ProjectDialogTarget | null>(null);
  return (
    <DataPage>
      <PageHeader>
        <PageHeaderHeading className="flex items-center gap-2">
          <PageHeaderTitle>{mode === "admin" ? "MCP 接入" : "我的 MCP 接入"}</PageHeaderTitle>
          <Tag tone="warning" mono>
            Beta
          </Tag>
        </PageHeaderHeading>
        <PageHeaderActions>
          <Button onClick={() => setTarget({ kind: "new" })}>
            <Plus />
            新建接入项目
          </Button>
        </PageHeaderActions>
      </PageHeader>

      <Notice tone="info" title="接入前须知" className="mb-4">
        工具集合与权限档位仍在 Beta，会随反馈调整。令牌代表把你<strong>自己名下</strong>的画布授权给
        外部工具，能看和改的范围由权限档位决定，具体哪块画布由每次调用指定（没指定就用默认画布）。
        勾了「生成素材」，出图、出视频、出配音花的是<strong>你名下</strong>的积分；删除节点不开放。
      </Notice>

      <DataPanel>
        <DataPanelBody>
          {state.loading ? (
            <p className="text-muted-foreground p-4 text-sm">加载中…</p>
          ) : state.listFailed ? (
            <Notice
              tone="danger"
              title="项目列表加载失败"
              className="m-4"
              action={
                <Button variant="outline" size="sm" onClick={() => void state.refresh()}>
                  重试
                </Button>
              }
            />
          ) : (
            <ProjectTable
              state={state}
              mode={mode}
              onEdit={(item) => setTarget({ kind: "edit", item })}
            />
          )}
        </DataPanelBody>
      </DataPanel>

      {selected && (
        <AccessPanel
          key={`${selected.id}:${selected.token?.id ?? "none"}`}
          state={state}
          project={selected}
          now={now}
        />
      )}

      <ProjectDialog target={target} state={state} onClose={() => setTarget(null)} />
    </DataPage>
  );
}
