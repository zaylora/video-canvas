import { Link, useParams } from "react-router";
import { Loader2, ShoppingBag } from "lucide-react";

import type { Project } from "@/api/marketing/type";
import { MarketingComposer } from "@/components/marketing/marketing-composer";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useMarketingOptions } from "@/hooks/use-marketing-options";
import { useMarketingProject } from "@/hooks/use-marketing-project";
import { useRemoteModels } from "@/hooks/use-models";
import { useCreditsStore } from "@/store/credits";

import { RoundSection } from "./round-section";
import { SideCard } from "./side-card";
import { useMarketingActions } from "./use-actions";

/** 项目已经拿到后的正文：左栏商品卡、右侧轮次（倒序）、底部吸附的生成框 */
function ProjectBody({ project }: { project: Project }) {
  const options = useMarketingOptions();
  const video = useRemoteModels("video");
  const available = useCreditsStore((state) => state.credits?.available ?? null);
  const actions = useMarketingActions(project);
  const scripting = project.rounds.some((round) => round.status === "scripting");
  const labelOf = (key: string) => video.models.find((item) => item.key === key)?.label ?? key;

  return (
    <div className="mx-auto grid w-full max-w-320 gap-6 px-4 pb-6 md:px-10 lg:grid-cols-[260px_minmax(0,1fr)]">
      <SideCard
        project={project}
        scripting={scripting}
        saving={!!actions.busy.facts}
        onSave={actions.saveFacts}
      />
      <div className="min-w-0">
        <div className="grid gap-10">
          {project.rounds.map((round) => (
            <RoundSection
              key={round.id}
              project={project}
              round={round}
              angles={options.angles}
              platforms={options.platforms}
              videoModelLabel={labelOf(round.settings.videoModel)}
              available={available}
              actions={actions}
            />
          ))}
        </div>
        <div className="sticky bottom-4 z-10 mt-8">
          <MarketingComposer project={project} />
        </div>
      </div>
    </div>
  );
}

/**
 * 营销片详情页（/marketing/:id）：左栏商品卡（事实卡可编辑），右侧按轮次倒序展示每一轮的
 * 设置、环节进度、参考拆解和脚本卡，底部贴着生成框在这个项目里再写一轮。
 * 数据读本地缓存，进页面拉一次，之后靠 WebSocket 推送；断线重连后重新拉详情对账。
 * 项目不存在或加载失败时显示失败态和返回按钮。
 */
export default function MarketingDetailPage() {
  const { id } = useParams();
  const { project, status, reload } = useMarketingProject(id);

  if (project) return <ProjectBody project={project} />;

  if (status === "error") {
    return (
      <div className="text-muted-foreground flex flex-col items-center gap-3 py-24 text-sm">
        <ShoppingBag className="size-8 opacity-50" />
        营销片不存在或加载失败
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={() => void reload()}>
            重试
          </Button>
          <Button variant="outline" size="sm" render={<Link to="/marketing" />}>
            返回营销工作室
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="mx-auto grid w-full max-w-320 gap-6 px-4 md:px-10 lg:grid-cols-[260px_minmax(0,1fr)]">
      <Skeleton className="h-96 rounded-3xl motion-reduce:animate-none" />
      <div className="grid gap-4">
        <div className="text-muted-foreground flex items-center gap-2 text-sm">
          <Loader2 className="size-4 animate-spin" />
          加载中
        </div>
        <Skeleton className="h-56 rounded-3xl motion-reduce:animate-none" />
      </div>
    </div>
  );
}
