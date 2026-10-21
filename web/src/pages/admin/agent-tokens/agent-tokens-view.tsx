import { KeyRound, Plus } from "lucide-react";
import { useState, type FormEvent } from "react";

import type { AgentTokenItem } from "@/api/admin/agent-token/type.d";
import { CopyButton } from "@/components/admin-ui/copy-button";
import { DataPage, DataPanel, DataPanelBody } from "@/components/admin-ui/data-panel";
import {
  EmptyState,
  EmptyStateDescription,
  EmptyStateIcon,
} from "@/components/admin-ui/empty-state";
import { FormField } from "@/components/admin-ui/form-field";
import { Notice } from "@/components/admin-ui/notice";
import { PageHeader, PageHeaderHeading, PageHeaderTitle } from "@/components/admin-ui/page-header";
import { RowAction } from "@/components/admin-ui/row-action";
import {
  SecretRevealCode,
  SecretRevealHint,
  SecretRevealValue,
} from "@/components/admin-ui/secret-reveal";
import { Tag, type TagTone } from "@/components/admin-ui/tag";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  TOKEN_DAYS_MAX,
  TOKEN_DAYS_MIN,
  TOKEN_STATUS_LABEL,
  formatTokenDay,
  formatTokenLastUsed,
  tokenStatus,
  type IssueFormValues,
  type TokenPageState,
  type TokenStatus,
} from "@/utils/admin/agent-token";

const STATUS_TONE: Record<TokenStatus, TagTone> = {
  active: "success",
  revoked: "neutral",
  expired: "warning",
};

/** 令牌页的全部输入：状态、表单和回调都由外面给，页面本身没有隐藏状态，便于单独验证 */
export type AgentTokensViewProps = {
  /** 列表、加载状态和刚签发的令牌 */
  state: TokenPageState;
  /** 签发表单里的值 */
  values: IssueFormValues;
  /** 表单校验错误 */
  errors: { name?: string; days?: string };
  /** 正在签发 */
  issuing: boolean;
  /** 当前时间（毫秒），用来判断过期 */
  now: number;
  onChange: (values: IssueFormValues) => void;
  onIssue: () => void;
  /** 点「我已保存」 */
  onDismiss: () => void;
  onRevoke: (item: AgentTokenItem) => void;
  /** 列表加载失败后重试 */
  onRetry: () => void;
};

/** 弹窗里的内容：明文、复制、保存说明。单独导出，便于不依赖弹窗外壳验证「明文只出现一次」 */
export function IssuedTokenContent({ token }: { token: string }) {
  return (
    <div data-slot="issued-token" className="flex flex-col gap-3">
      <SecretRevealValue>
        <SecretRevealCode>{token}</SecretRevealCode>
        <CopyButton text={token} variant="outline" />
      </SecretRevealValue>
      <SecretRevealHint>
        写到本机的 <code>.traces/.env</code>：<code>VC_TRACE_TOKEN=…</code>（该文件已加入
        gitignore）
      </SecretRevealHint>
    </div>
  );
}

/**
 * 刚签发的令牌：用弹窗显示，明文只在这里出现一次，关掉就没了。
 * 用 AlertDialog：点遮罩不会关；Esc 和关闭按钮也不响应，只有「我已保存」能关，避免手滑关掉后再也找不回明文。
 * 退出动画播完后才清掉持有的明文，动画期间弹窗不会闪成空的。
 */
export function IssuedTokenDialog({
  token,
  onDismiss,
}: {
  token: string | null;
  onDismiss: () => void;
}) {
  const [shown, setShown] = useState<string | null>(token);
  if (token && token !== shown) setShown(token);
  return (
    <AlertDialog
      open={token !== null}
      onOpenChange={() => undefined}
      onOpenChangeComplete={(open) => !open && setShown(null)}
    >
      <AlertDialogContent className="data-[size=default]:sm:max-w-lg">
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <KeyRound className="text-status-success size-4" aria-hidden="true" />
            令牌已签发
          </AlertDialogTitle>
          <AlertDialogDescription>明文只显示这一次，关闭后无法再查看。</AlertDialogDescription>
        </AlertDialogHeader>
        {shown && <IssuedTokenContent token={shown} />}
        <AlertDialogFooter>
          <AlertDialogAction onClick={onDismiss}>我已保存</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

/** 签发表单：用途 + 有效期（天） */
function IssueForm({
  values,
  errors,
  issuing,
  onChange,
  onIssue,
}: Pick<AgentTokensViewProps, "values" | "errors" | "issuing" | "onChange" | "onIssue">) {
  const submit = (event: FormEvent) => {
    event.preventDefault();
    onIssue();
  };
  return (
    <form
      data-slot="token-issue-form"
      onSubmit={submit}
      noValidate
      className="bg-card mb-4 grid gap-3 rounded-xl border p-4"
    >
      <h2 className="text-sm font-semibold">签发新令牌</h2>
      <div className="flex flex-wrap items-start gap-3">
        <FormField
          label="用途"
          htmlFor="token-name"
          error={errors.name}
          className="min-w-48 flex-1"
        >
          <Input
            id="token-name"
            value={values.name}
            placeholder="如：本机 Codex"
            aria-invalid={!!errors.name}
            onChange={(e) => onChange({ ...values, name: e.target.value })}
          />
        </FormField>
        <FormField
          label={`有效期（天，${TOKEN_DAYS_MIN}–${TOKEN_DAYS_MAX}）`}
          htmlFor="token-days"
          error={errors.days}
          className="w-36"
        >
          <Input
            id="token-days"
            type="number"
            inputMode="numeric"
            min={TOKEN_DAYS_MIN}
            max={TOKEN_DAYS_MAX}
            value={values.days}
            aria-invalid={!!errors.days}
            onChange={(e) => onChange({ ...values, days: e.target.value })}
          />
        </FormField>
        <Button type="submit" disabled={issuing} className="mt-[22px]">
          <Plus />
          签发
        </Button>
      </div>
    </form>
  );
}

/** 令牌表格；一枚都没有时显示空状态 */
function TokenTable({
  items,
  now,
  onRevoke,
}: Pick<AgentTokensViewProps, "now" | "onRevoke"> & { items: AgentTokenItem[] }) {
  if (items.length === 0) {
    return (
      <EmptyState className="m-4">
        <EmptyStateIcon>
          <KeyRound />
        </EmptyStateIcon>
        <EmptyStateDescription>
          还没有令牌。签发一个，供本地 agent-trace 使用。
        </EmptyStateDescription>
      </EmptyState>
    );
  }
  return (
    <Table>
      <TableHeader>
        <TableRow className="bg-muted/40 hover:bg-muted/40">
          <TableHead className="px-4">用途</TableHead>
          <TableHead>前缀</TableHead>
          <TableHead>到期</TableHead>
          <TableHead>最近使用</TableHead>
          <TableHead>状态</TableHead>
          <TableHead className="w-20 px-4 text-right">操作</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((item) => {
          const status = tokenStatus(item, now);
          return (
            <TableRow key={item.id} data-token-id={item.id}>
              <TableCell className="px-4 py-3 font-medium">{item.name}</TableCell>
              <TableCell className="py-3 font-mono text-xs">{item.prefix}…</TableCell>
              <TableCell className="py-3 tabular-nums">{formatTokenDay(item.expires_at)}</TableCell>
              <TableCell className="text-muted-foreground py-3 tabular-nums">
                {formatTokenLastUsed(item.last_used_at)}
              </TableCell>
              <TableCell className="py-3">
                <Tag tone={STATUS_TONE[status]}>{TOKEN_STATUS_LABEL[status]}</Tag>
              </TableCell>
              <TableCell className="px-4 py-3 text-right">
                {!item.revoked_at && (
                  <RowAction
                    tone="danger"
                    aria-label={`注销 ${item.name}`}
                    onClick={() => onRevoke(item)}
                  >
                    注销
                  </RowAction>
                )}
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}

/**
 * Agent 分析令牌页的画面：说明、（刚签发时的）明文提示块、签发表单、令牌表格。
 * 令牌给本地 agent-trace 只读拉取运行记录；明文只在刚签发时显示一次。
 */
export function AgentTokensView({
  state,
  values,
  errors,
  issuing,
  now,
  onChange,
  onIssue,
  onDismiss,
  onRevoke,
  onRetry,
}: AgentTokensViewProps) {
  return (
    <DataPage>
      <PageHeader>
        <PageHeaderHeading>
          <PageHeaderTitle>分析令牌</PageHeaderTitle>
        </PageHeaderHeading>
      </PageHeader>

      <IssuedTokenDialog token={state.issued?.token ?? null} onDismiss={onDismiss} />
      <IssueForm
        values={values}
        errors={errors}
        issuing={issuing}
        onChange={onChange}
        onIssue={onIssue}
      />

      <DataPanel>
        <DataPanelBody>
          {state.status === "error" ? (
            <Notice
              tone="danger"
              title="令牌列表加载失败"
              className="m-4"
              action={
                <Button variant="outline" size="sm" onClick={onRetry}>
                  重试
                </Button>
              }
            />
          ) : (
            <TokenTable items={state.items} now={now} onRevoke={onRevoke} />
          )}
        </DataPanelBody>
      </DataPanel>
    </DataPage>
  );
}
