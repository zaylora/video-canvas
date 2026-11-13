import { useState } from "react";
import { Box, BookOpen, ChevronRight, CodeXml, Eye, Film, ShoppingBag } from "lucide-react";

import type { FormatterInfo, StepSummary } from "@/api/admin/steps/type";
import { DataPage } from "@/components/admin-ui/data-panel";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { LoadFailed } from "@/components/admin-ui/load-failed";
import { Notice } from "@/components/admin-ui/notice";
import { PageHeader, PageHeaderHeading, PageHeaderTitle } from "@/components/admin-ui/page-header";
import { GLASS_PANEL } from "@/components/admin-ui/glass";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatCanvasTime } from "@/utils/home/home";
import { bindingLabel, fallbackSteps } from "@/utils/admin/steps";

import { MEDIA_LABEL } from "./step-tabs";
import { StepSheet } from "./step-sheet";
import { useSteps } from "./use-steps";

/** 短剧工作室的四个环节：还没迁移到环节协议层，灰显，沿用开拍设置里的「内置 / 我的 / 技能」 */
const DRAMA_STEPS: Array<[string, string]> = [
  ["drama.plan", "剧本改编"],
  ["drama.breakdown", "资产拆解"],
  ["drama.asset_prompts", "资产提示词"],
  ["drama.storyboard", "分镜脚本"],
];

/** 一个面板：图标 + 标题 + 角标，下面是表格 */
function Panel({
  icon: Icon,
  title,
  badge,
  children,
}: {
  icon: typeof Film;
  title: string;
  badge?: string;
  children: React.ReactNode;
}) {
  return (
    <section className={`${GLASS_PANEL} overflow-hidden`}>
      <div className="flex items-center gap-2 px-4 py-3 text-sm font-medium">
        <Icon className="size-4" />
        {title}
        {badge && <Tag>{badge}</Tag>}
      </div>
      <div className="border-chrome-border border-t">{children}</div>
    </section>
  );
}

/** 近 7 天失败率：0–1 转百分比 */
const failRate = (value: number) => `${(value * 100).toFixed(1).replace(/\.0$/, "")}%`;

/** 营销工作室的四行：环节名 + key、当前方法（内置 / 技能名 vN，有新版本带角标，退回内置带提示）、输入媒体、最近试跑、近 7 天失败率 */
function MarketingTable({
  steps,
  onOpen,
}: {
  steps: StepSummary[];
  onOpen: (key: string) => void;
}) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>环节</TableHead>
          <TableHead>当前方法</TableHead>
          <TableHead>输入媒体</TableHead>
          <TableHead>最近试跑</TableHead>
          <TableHead>近 7 天失败率</TableHead>
          <TableHead className="w-10" />
        </TableRow>
      </TableHeader>
      <TableBody>
        {steps.map((step) => {
          const fallback = !!step.effective.fallback;
          return (
            <TableRow
              key={step.key}
              tabIndex={0}
              className="focus-visible:bg-chrome-hover cursor-pointer outline-none"
              onClick={() => onOpen(step.key)}
              onKeyDown={(event) => {
                if (event.key === "Enter") onOpen(step.key);
              }}
            >
              <TableCell>
                <div className="font-medium">{step.title}</div>
                <div className="text-muted-foreground font-mono text-xs">{step.key}</div>
              </TableCell>
              <TableCell>
                <span className="inline-flex flex-wrap items-center gap-1.5">
                  <span className="inline-flex items-center gap-1.5 text-sm">
                    {step.effective.source === "builtin" || fallback ? (
                      <Box className="size-4" />
                    ) : (
                      <BookOpen className="size-4" />
                    )}
                    {fallback ? "内置方法" : bindingLabel(step.binding)}
                  </span>
                  {fallback && <Tag tone="warning">已退回内置</Tag>}
                  {!fallback && step.newer_version !== null && (
                    <Tag tone="violet" title={`技能已发布 v${step.newer_version}`}>
                      有新版本 v{step.newer_version}
                    </Tag>
                  )}
                </span>
              </TableCell>
              <TableCell className="text-muted-foreground">{MEDIA_LABEL[step.media]}</TableCell>
              <TableCell>
                {step.last_trial ? (
                  <span className="inline-flex items-center gap-1.5">
                    <Tag tone={step.last_trial.passed ? "success" : "danger"}>
                      {step.last_trial.passed ? "通过" : "未通过"}
                    </Tag>
                    <span className="text-muted-foreground text-xs tabular-nums">
                      {formatCanvasTime(step.last_trial.created_at)}
                    </span>
                  </span>
                ) : (
                  <span className="text-muted-foreground">—</span>
                )}
              </TableCell>
              <TableCell className="tabular-nums">{failRate(step.fail_rate_7d)}</TableCell>
              <TableCell className="text-muted-foreground text-right">
                <ChevronRight className="size-4" />
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}

/** 格式化器预览：同一份脚本经代码拼成视频模型的提示词，只读 */
function FormatterDialog({
  formatter,
  onClose,
}: {
  formatter: FormatterInfo | null;
  onClose: () => void;
}) {
  return (
    <Dialog open={!!formatter} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>格式化器预览 · {formatter?.key}</DialogTitle>
          <DialogDescription>
            同一份脚本，经代码拼装成视频模型的提示词。它不读技能，换技能不影响这里；改写法需要改代码发版。
          </DialogDescription>
        </DialogHeader>
        <pre className="bg-muted/60 max-h-[55vh] overflow-auto rounded-xl p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap">
          {formatter?.sample}
        </pre>
        <DialogFooter>
          <Button size="sm" onClick={onClose}>
            好的
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * 后台「环节技能」页（/admin/agent/steps）：给每个写脚本的环节挑方法（内置或导入的技能）。
 * 营销工作室四行可点开抽屉（协议 / 方法 / 试跑）；短剧工作室四行灰显「待迁移」；
 * 视频提示词格式化器只读三行，可预览。有环节因技能停用被自动退回内置时，页顶出提示条。
 */
export default function AgentStepsPage() {
  const { steps, formatters, status, reload } = useSteps();
  const [openKey, setOpenKey] = useState<string | null>(null);
  const [formatter, setFormatter] = useState<FormatterInfo | null>(null);
  const fallback = fallbackSteps(steps);

  return (
    <DataPage>
      <PageHeader>
        <PageHeaderHeading>
          <PageHeaderTitle>环节技能</PageHeaderTitle>
        </PageHeaderHeading>
      </PageHeader>

      {fallback.length > 0 && (
        <Notice
          tone="warning"
          className="mb-4"
          action={
            <Button variant="outline" size="sm" onClick={() => setOpenKey(fallback[0].key)}>
              去处理
            </Button>
          }
        >
          {fallback.map((step) => `「${step.title}」`).join("")}绑定的技能{" "}
          {fallback[0].binding.skill_name} 已停用，已自动退回内置方法
        </Notice>
      )}

      <div className="grid gap-4">
        <Panel icon={ShoppingBag} title="营销工作室">
          {status === "loading" ? (
            <div className="grid gap-2 p-4">
              {[0, 1, 2, 3].map((i) => (
                <Skeleton key={i} className="h-12 rounded-xl" />
              ))}
            </div>
          ) : status === "error" ? (
            <LoadFailed onRetry={() => void reload()} />
          ) : (
            <MarketingTable steps={steps} onOpen={setOpenKey} />
          )}
        </Panel>

        <Panel icon={Film} title="短剧工作室" badge="待迁移">
          <Table>
            <TableBody>
              {DRAMA_STEPS.map(([key, title]) => (
                <TableRow key={key} className="opacity-55 hover:bg-transparent">
                  <TableCell>
                    <div>{title}</div>
                    <div className="text-muted-foreground font-mono text-xs">{key}</div>
                  </TableCell>
                  <TableCell>沿用开拍设置里的「内置 / 我的 / 技能」</TableCell>
                  <TableCell className="text-right">—</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Panel>

        <Panel icon={CodeXml} title="视频提示词格式化器" badge="代码维护">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>写法</TableHead>
                <TableHead>名称</TableHead>
                <TableHead>结构</TableHead>
                <TableHead className="w-24" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {formatters.map((item) => (
                <TableRow key={item.key}>
                  <TableCell className="font-mono text-xs">{item.key}</TableCell>
                  <TableCell>{item.name}</TableCell>
                  <TableCell className="text-muted-foreground text-xs">
                    {item.description}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button variant="ghost" size="xs" onClick={() => setFormatter(item)}>
                      <Eye />
                      预览
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Panel>
      </div>

      <StepSheet
        stepKey={openKey}
        summary={steps.find((step) => step.key === openKey)}
        onClose={() => setOpenKey(null)}
        onBound={() => void reload()}
      />
      <FormatterDialog formatter={formatter} onClose={() => setFormatter(null)} />
    </DataPage>
  );
}
