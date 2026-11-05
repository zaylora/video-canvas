import { Check, Plus } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

import { GLASS_PANEL } from "@/components/admin-ui/glass";

/** 上线进度的一步 */
export type OnboardingStep = {
  /** 步骤名 */
  title: string;
  /** 这一步是否已完成 */
  ok: boolean;
  /** 大卡里的主数字，如“3 个可用” */
  stat: string;
  /** 简写，如“3 个插件” */
  short: string;
  /** 这一步的操作按钮；没有权限时为 null */
  action: {
    label: string;
    to: string;
    /** 跳转时带的路由 state，用来让目标页直接打开弹窗 */ state?: unknown;
  } | null;
};

/**
 * 上线进度引导：接入平台 → 配置渠道 → 上线模型。
 * - 有一步没完成：显示三张清透大卡，卡住的那一步描边加主按钮；
 * - 三步都完成：整块不显示（下面的指标已经写了插件、渠道、模型的数量，不重复占一行）。
 * @param steps 三个步骤
 * @param ready 清单都已加载，没加载完显示骨架
 * @param onNavigate 点步骤上的按钮
 */
export function OnboardingSteps({
  steps,
  ready,
  onNavigate,
}: {
  steps: OnboardingStep[];
  ready: boolean;
  onNavigate: (to: string, state?: unknown) => void;
}) {
  const current = steps.find((step) => !step.ok);

  if (!ready) return <Skeleton className="mb-4 h-14 rounded-3xl" />;
  if (!current) return null;

  return (
    <section aria-label="上线进度" className="mb-4 grid gap-4 md:grid-cols-3">
      {steps.map((step, index) => (
        <Card
          key={step.title}
          className={cn(
            GLASS_PANEL,
            "gap-3",
            current.title === step.title && "ring-primary ring-2",
          )}
        >
          <CardHeader className="flex-row items-center gap-2">
            <span
              className={cn(
                "grid size-6 shrink-0 place-items-center rounded-full text-xs font-semibold",
                step.ok ? "bg-status-success text-background" : "bg-chrome-hover",
              )}
            >
              {step.ok ? <Check className="size-3.5" strokeWidth={3} /> : index + 1}
            </span>
            <CardTitle className="text-sm">{step.title}</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col items-start gap-3">
            <span className="text-2xl font-semibold tabular-nums">{step.stat}</span>
            {step.action && (
              <Button
                size="sm"
                variant={current.title === step.title ? "default" : "outline"}
                className="rounded-full"
                onClick={() => onNavigate(step.action!.to, step.action!.state)}
              >
                <Plus />
                {step.action.label}
              </Button>
            )}
          </CardContent>
        </Card>
      ))}
    </section>
  );
}
