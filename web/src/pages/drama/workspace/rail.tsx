import { Link } from "react-router";
import { motion } from "motion/react";
import { ArrowLeft, BookOpen, Boxes, Film, type LucideIcon } from "lucide-react";

import { SPRING } from "@/lib/motion";
import { cn } from "@/lib/utils";

import type { WorkspaceArea } from "./use-workspace";

const ITEMS: { key: WorkspaceArea; label: string; icon: LucideIcon }[] = [
  { key: "script", label: "剧本", icon: BookOpen },
  { key: "assets", label: "资产", icon: Boxes },
  { key: "episodes", label: "分集", icon: Film },
];

/** 左导航：剧本 / 资产 / 分集，底部返回短剧列表。选中滑块用 layoutId 在三项之间滑动 */
export function Rail({
  area,
  onChange,
}: {
  area: WorkspaceArea;
  onChange: (area: WorkspaceArea) => void;
}) {
  const item =
    "focus-visible:ring-ring/50 relative flex h-14 w-full flex-col items-center justify-center gap-1 rounded-xl text-[11px] outline-none focus-visible:ring-2 [&_svg]:size-5";
  return (
    <nav
      aria-label="工作台导航"
      className="bg-sidebar border-sidebar-border flex w-16 shrink-0 flex-col items-center gap-1 border-r p-2 max-sm:w-14"
    >
      {ITEMS.map(({ key, label, icon: Icon }) => {
        const active = area === key;
        return (
          <button
            key={key}
            type="button"
            aria-current={active}
            onClick={() => onChange(key)}
            className={cn(
              item,
              active ? "text-foreground" : "text-muted-foreground hover:text-foreground",
            )}
          >
            {active && (
              <motion.span
                layoutId="drama-rail-active"
                transition={SPRING}
                className="bg-sidebar-accent absolute inset-0 rounded-xl"
              />
            )}
            <Icon className="relative" />
            <span className="relative">{label}</span>
          </button>
        );
      })}
      <div className="flex-1" />
      <Link
        to="/drama"
        title="返回短剧列表"
        className={cn(item, "text-muted-foreground hover:text-foreground")}
      >
        <ArrowLeft />
        返回
      </Link>
    </nav>
  );
}
