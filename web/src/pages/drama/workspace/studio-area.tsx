import { Clapperboard, Lock, ScrollText } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useDramaVideoModel } from "@/hooks/use-drama-video-model";
import { episodeCardView } from "@/utils/drama/episode-cards";
import { busyReason } from "@/utils/drama/jobs";
import type { WorkspaceStage } from "@/utils/drama/stages";

import { StudioView } from "../studio/view";
import { useWorkspace } from "./context";
import { StoryboardButton, useStoryboardStart } from "./storyboard-start";

/** 前置工序没做完时的引导：一个图标、一句话、一个按钮 */
function Gate({
  icon,
  title,
  text,
  children,
}: {
  icon: React.ReactNode;
  title: string;
  text: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
      <span className="bg-muted text-muted-foreground grid size-16 place-items-center rounded-[20px] [&_svg]:size-7">
        {icon}
      </span>
      <h3 className="text-[17px] font-semibold">{title}</h3>
      <p className="text-muted-foreground max-w-[44ch] text-[13px]">{text}</p>
      {children}
    </div>
  );
}

/**
 * 片场：当前集的片段制作。
 * 还没拆解资产 → 引导去拆解；还没有分镜脚本 → 引导生成（带积分估价）；分镜生成中 → 等待；
 * 分镜就绪 → 进入片场（资产库 / 播放器 / 检查器 / 时间线）。
 */
export function StudioArea({
  ep,
  onGo,
}: {
  ep: number;
  onGo: (stage: WorkspaceStage, ep?: number) => void;
}) {
  const { drama, jobs, refresh } = useWorkspace();
  const video = useDramaVideoModel(drama.id);
  const starter = useStoryboardStart(video.maxSeconds);
  const episode = drama.episodes.find((item) => item.no === ep) ?? drama.episodes[0];

  if (!episode) {
    return (
      <Gate
        icon={<Clapperboard />}
        title="还没有分集"
        text="研读完成后分集会出现在左侧，也可以在剧本页手动新增一集。"
      />
    );
  }

  const { action } = episodeCardView(episode, { busyReason: busyReason(jobs) });

  if (action.kind === "enter") {
    // 换一集整块重来，不让上一集的选中片段、草稿残留
    return (
      <StudioView
        key={`${drama.id}:${episode.no}`}
        drama={drama}
        no={episode.no}
        refresh={refresh}
      />
    );
  }

  if (action.kind === "breakdown") {
    const hasScript = episode.scriptStatus === "ready";
    return (
      <Gate
        icon={<Lock />}
        title={`先拆解第 ${episode.no} 集的资产`}
        text={`分镜需要知道这一集出场的角色、场景和道具。${hasScript ? "拆解后这一集的剧本会锁定。" : "这一集的剧本也还没写好。"}`}
      >
        <Button onClick={() => onGo("script", episode.no)}>
          <ScrollText />
          {hasScript ? "去拆解" : "去写剧本"}
        </Button>
      </Gate>
    );
  }

  if (action.kind === "generating") {
    return (
      <Gate
        icon={<Clapperboard className="animate-pulse motion-reduce:animate-none" />}
        title={`正在生成第 ${episode.no} 集的分镜脚本…`}
        text="把剧本拆成每段不超过模型时长上限的片段，通常一两分钟。完成后这里会自动出现片场。"
      />
    );
  }

  return (
    <Gate
      icon={<Clapperboard />}
      title={`第 ${episode.no} 集还没有分镜`}
      text="根据剧本把这一集拆成若干片段，镜头里会自动带上已拆解的角色、场景和道具。"
    >
      <StoryboardButton
        episode={episode}
        label={action.label}
        disabledReason={action.disabledReason}
        maxSeconds={video.maxSeconds}
        busy={starter.pending === episode.no}
        onStart={() => void starter.start(episode)}
      />
    </Gate>
  );
}
