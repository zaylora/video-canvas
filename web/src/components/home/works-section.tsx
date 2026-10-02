import { useId, useState } from "react";
import { motion } from "motion/react";
import { Plus, Search } from "lucide-react";

import { SoonTip } from "@/components/home/soon";
import { Button } from "@/components/ui/button";
import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";

const CATEGORIES = [
  "全部",
  "2026 赛事",
  "影视长片",
  "创意短片",
  "动画 CG",
  "AIMV",
  "短剧漫剧",
  "其他",
];

/**
 * 首页的作品广场（占位）：分类能切，内容一律是「即将上线」空状态，
 * 搜索和发布作品先禁用。等有作品接口后再接数据。
 */
export function WorksSection() {
  const [category, setCategory] = useState(CATEGORIES[0]);
  const layoutId = useId();

  return (
    <section aria-label="作品广场" className="mt-7">
      <div className="mb-4 flex items-center gap-3">
        <div role="tablist" className="-mx-1 flex min-w-0 flex-1 gap-1 overflow-x-auto px-1">
          {CATEGORIES.map((item) => {
            const selected = item === category;
            return (
              <button
                key={item}
                type="button"
                role="tab"
                aria-selected={selected}
                onClick={() => setCategory(item)}
                className={cn(
                  "text-muted-foreground hover:text-foreground focus-visible:ring-ring/50 relative h-10 shrink-0 rounded-[10px] px-3.5 text-[15px] whitespace-nowrap outline-none transition-colors duration-150 focus-visible:ring-3",
                  selected && "bg-muted/60 text-foreground font-semibold",
                )}
              >
                {item}
                {selected && (
                  <motion.span
                    layoutId={layoutId}
                    transition={SPRING}
                    className="bg-foreground absolute inset-x-3.5 bottom-0.5 h-0.5 rounded-full"
                  />
                )}
              </button>
            );
          })}
        </div>
        <SoonTip className="cursor-not-allowed max-md:hidden">
          <label className="bg-muted/60 text-muted-foreground flex h-10 w-52 items-center gap-2 rounded-full px-4 opacity-50">
            <Search className="size-4" />
            <input
              disabled
              placeholder="搜索作品"
              className="min-w-0 flex-1 bg-transparent outline-none"
            />
          </label>
        </SoonTip>
        <SoonTip>
          <Button disabled size="lg" className="rounded-full font-semibold">
            <Plus />
            发布作品
          </Button>
        </SoonTip>
      </div>
      <div className="border-border flex h-80 flex-col items-center justify-center gap-1.5 rounded-3xl border bg-[radial-gradient(circle,var(--canvas-dot)_1px,transparent_1.3px)] bg-size-[20px_20px] text-center">
        <b className="text-[15px] font-semibold">作品广场即将上线</b>
        <span className="text-muted-foreground text-[13px]">这里会展示大家用画布做出来的作品</span>
      </div>
    </section>
  );
}
