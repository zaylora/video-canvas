import { Check, ChevronDown, Plus } from "lucide-react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { useId, useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { DURATION, EASE_OUT } from "@/lib/motion";
import { cn } from "@/lib/utils";

/** 上线进度的一步 */
export type OnboardingStep = {
  /** 步骤名 */
  title: string;
  /** 步骤说明，如“插件” */
  hint: string;
  /** 这一步是否已完成 */
  ok: boolean;
  /** 大卡里的主数字，如“3 个可用” */
  stat: string;
  /** 细条里的简写，如“3 个插件” */
  short: string;
  /** 这一步的操作按钮；没有权限时为 null */
  action: { label: string; to: string } | null;
};

/**
 * 上线进度引导：接入平台 → 配置渠道 → 上线模型。
 * - 有一步没完成：直接显示三张大卡，卡住的那一步描边加主按钮，不提供收起（还没配好时引导就该摆在眼前）；
 * - 三步都完成：细条常驻，大卡默认收起，点开后从细条下方往下拉出来，再点往上收回去；
 *   细条左侧收起时是一行打勾的步骤摘要，展开时换成“上线进度”标题（大卡已经是同样的信息，不重复），快速淡入淡出切换；
 * - 往下拉 = 高度 0→auto + 透明度（DURATION.slow / EASE_OUT），收起快一档（slowExit）；箭头同步翻转 180°。
 *   下面的内容是被这块高度自然推下去的，页面里不要再给它们套 layout 动画，否则会和这里的高度动画互相拉扯；
 * - 开了“减少动态效果”：不做高度变化，只淡入淡出。
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
  onNavigate: (to: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const panelId = useId();
  const reduced = useReducedMotion();
  const allOk = steps.every((step) => step.ok);
  const current = steps.find((step) => !step.ok);

  if (!ready) {
    return (
      <Card size="sm" className="mb-3">
        <CardContent>
          <Skeleton className="h-5 w-3/5" />
        </CardContent>
      </Card>
    );
  }

  const cards = (
    <div className="grid gap-3 md:grid-cols-3">
      {steps.map((step, index) => (
        <Card
          key={step.title}
          className={cn("gap-3", current?.title === step.title && "ring-primary ring-2")}
        >
          <CardHeader className="flex-row items-center gap-2">
            <span
              className={cn(
                "grid size-6 shrink-0 place-items-center rounded-full text-xs font-semibold",
                step.ok ? "bg-emerald-500 text-white" : "bg-muted",
              )}
            >
              {step.ok ? <Check className="size-3.5" /> : index + 1}
            </span>
            <CardTitle className="text-sm">{step.title}</CardTitle>
            <span className="text-muted-foreground text-xs">{step.hint}</span>
          </CardHeader>
          <CardContent className="flex flex-col items-start gap-3">
            <span className="text-2xl font-semibold tabular-nums">{step.stat}</span>
            {step.action && (
              <Button
                size="sm"
                variant={current?.title === step.title ? "default" : "outline"}
                onClick={() => onNavigate(step.action!.to)}
              >
                <Plus />
                {step.action.label}
              </Button>
            )}
          </CardContent>
        </Card>
      ))}
    </div>
  );

  if (!allOk) {
    return (
      <section aria-label="上线进度" className="mb-3">
        {cards}
      </section>
    );
  }

  return (
    <section aria-label="上线进度" className="mb-3">
      <Card size="sm">
        <CardContent className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <div className="min-w-0 flex-1">
            {/* 收起时给步骤摘要；展开时下面的大卡已经是同样的信息，这里只留标题，免得重复 */}
            <AnimatePresence mode="wait" initial={false}>
              <motion.div
                key={open ? "title" : "summary"}
                initial={{ opacity: 0 }}
                animate={{ opacity: 1, transition: { duration: DURATION.fast, ease: EASE_OUT } }}
                exit={{ opacity: 0, transition: { duration: DURATION.fast, ease: EASE_OUT } }}
              >
                {open ? (
                  <p className="text-sm">
                    上线进度
                    <span className="text-muted-foreground ml-2 text-xs">三步都已完成</span>
                  </p>
                ) : (
                  <ol className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
                    {steps.map((step, index) => (
                      <li key={step.title} className="flex items-center gap-3">
                        {index > 0 && (
                          <span aria-hidden className="text-muted-foreground">
                            ›
                          </span>
                        )}
                        <span className="flex items-center gap-1.5">
                          <span className="grid size-5 place-items-center rounded-full bg-emerald-500 text-white">
                            <Check className="size-3" strokeWidth={3} />
                          </span>
                          {step.title}
                          <span className="text-muted-foreground text-xs tabular-nums">
                            {step.short}
                          </span>
                        </span>
                      </li>
                    ))}
                  </ol>
                )}
              </motion.div>
            </AnimatePresence>
          </div>
          <Button
            size="sm"
            variant="ghost"
            aria-expanded={open}
            aria-controls={panelId}
            onClick={() => setOpen((value) => !value)}
          >
            <motion.span
              aria-hidden
              className="inline-flex"
              animate={{ rotate: open ? 180 : 0 }}
              transition={{ duration: DURATION.base, ease: EASE_OUT }}
            >
              <ChevronDown />
            </motion.span>
            {open ? "收起引导" : "展开引导"}
          </Button>
        </CardContent>
      </Card>

      <AnimatePresence initial={false}>
        {open && (
          <motion.div
            key="cards"
            id={panelId}
            initial={reduced ? { opacity: 0 } : { height: 0, opacity: 0 }}
            animate={
              reduced
                ? { opacity: 1, transition: { duration: DURATION.base } }
                : {
                    height: "auto",
                    opacity: 1,
                    transition: {
                      height: { duration: DURATION.slow, ease: EASE_OUT },
                      opacity: { duration: DURATION.base, ease: EASE_OUT, delay: 0.04 },
                    },
                  }
            }
            exit={
              reduced
                ? { opacity: 0, transition: { duration: DURATION.exit } }
                : {
                    height: 0,
                    opacity: 0,
                    transition: {
                      height: { duration: DURATION.slowExit, ease: EASE_OUT },
                      opacity: { duration: DURATION.exit, ease: EASE_OUT },
                    },
                  }
            }
            /* overflow-hidden 会把卡片外圈的 1px 描边裁掉：左右下各撑出 4px 的裁剪余量，布局尺寸不变 */
            className="-mx-1 -mb-1 overflow-hidden px-1 pb-1"
          >
            <div className="pt-3">{cards}</div>
          </motion.div>
        )}
      </AnimatePresence>
    </section>
  );
}
