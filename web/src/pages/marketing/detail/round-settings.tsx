import { PenLine } from "lucide-react";

import type { Project, Round } from "@/api/marketing/type";
import { ModelMenu } from "@/components/drama/video-model-menu";
import { ChipPopover } from "@/components/drama/pickers";
import { useRemoteModels } from "@/hooks/use-models";
import { cn } from "@/lib/utils";
import { modelSummary } from "@/utils/drama/video-models";
import {
  resolutionOptions,
  roundModelSupport,
  roundSpec,
  switchedRoundParams,
} from "@/utils/marketing/composer";

import type { MarketingActions } from "./use-actions";

/** 弹层里的一行：小标题 + 选项 */
function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="grid gap-1.5">
      <span className="text-muted-foreground text-xs">{label}</span>
      <div className="flex flex-wrap gap-1.5">{children}</div>
    </div>
  );
}

/** 弹层里的一个选项胶囊；locked 时只展示当前值，不能点 */
function Option({
  active,
  locked,
  title,
  onClick,
  children,
}: {
  active: boolean;
  locked?: boolean;
  title?: string;
  onClick?: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={active}
      disabled={locked}
      title={title}
      onClick={onClick}
      className={cn(
        "ring-chrome-border focus-visible:ring-ring/50 inline-flex h-8 items-center rounded-full px-3 text-[13px] tabular-nums outline-none ring-1 transition-colors duration-120 focus-visible:ring-2",
        active ? "bg-chrome-hover text-foreground font-medium" : "text-muted-foreground",
        !locked && !active && "hover:bg-chrome-hover",
        locked && "cursor-default",
      )}
    >
      {children}
    </button>
  );
}

/**
 * 顶栏的本轮出片设置：视频模型、规格（画幅 · 分辨率 · 时长）两个可点的胶囊。
 * 写完脚本后只能换模型和分辨率；画幅、时长决定分镜，锁住，要换走「再写一轮」。
 * 只影响之后的出片；写脚本中不能改。
 */
export function RoundSettingsMenus({
  project,
  round,
  actions,
  onComposer,
}: {
  project: Project;
  round: Round;
  actions: MarketingActions;
  onComposer: () => void;
}) {
  const video = useRemoteModels("video");
  const settings = round.settings;
  const spec = roundSpec(settings);
  const current = video.models.find((item) => item.key === settings.videoModel);
  const busy = !!actions.busy[`settings:${round.id}`];
  const disabled = round.status === "scripting" || busy;
  const images = project.productAssets.length + (round.modelAsset ? 1 : 0);
  const support = (info: Parameters<typeof roundModelSupport>[0]) =>
    roundModelSupport(info, {
      images,
      hasRef: !!round.refVideo,
      aspectRatio: spec.aspectRatio,
      duration: spec.duration,
    });

  const pickModel = (key: string) => {
    if (key === settings.videoModel) return;
    const info = video.models.find((item) => item.key === key);
    if (!info) return;
    void actions.updateSettings(round, {
      videoModel: key,
      params: switchedRoundParams(info.capabilities, spec),
    });
  };

  const pickResolution = (resolution: string) => {
    if (resolution === spec.resolution) return;
    void actions.updateSettings(round, {
      videoModel: settings.videoModel,
      params: { ...settings.params, resolution },
    });
  };

  const resolutions = resolutionOptions(current?.capabilities);
  const specLabel = [
    spec.aspectRatio,
    spec.resolution?.toUpperCase(),
    spec.duration ? `${spec.duration}s` : undefined,
  ]
    .filter(Boolean)
    .join(" · ");
  const lockedTitle = "决定分镜，改它请再写一轮";

  return (
    <>
      {disabled ? (
        <span className="ring-chrome-border text-foreground/85 inline-flex h-8 items-center rounded-full px-3 text-[13px] ring-1">
          {current?.label ?? settings.videoModel}
        </span>
      ) : (
        <ModelMenu
          models={video.models}
          loading={video.status === "loading" || video.status === "idle"}
          model={current}
          onPick={pickModel}
          support={support}
          summary={modelSummary}
          kindName="视频"
          variant="chip"
          title="出片用的视频模型"
        />
      )}
      {specLabel && (
        <ChipPopover
          variant="chip"
          label={specLabel}
          disabled={disabled}
          title="出片规格"
          width="w-72"
        >
          {(close) => (
            <div className="grid gap-3.5 p-1.5">
              {spec.aspectRatio && (
                <Row label="画幅">
                  <Option active locked title={lockedTitle}>
                    {spec.aspectRatio}
                  </Option>
                </Row>
              )}
              {resolutions.length > 0 && (
                <Row label="分辨率">
                  {resolutions.map((item) => (
                    <Option
                      key={item}
                      active={item === spec.resolution}
                      onClick={() => {
                        close();
                        pickResolution(item);
                      }}
                    >
                      {item.toUpperCase()}
                    </Option>
                  ))}
                </Row>
              )}
              {spec.duration !== undefined && (
                <Row label="时长">
                  <Option active locked title={lockedTitle}>
                    {spec.duration}s
                  </Option>
                </Row>
              )}
              <button
                type="button"
                onClick={() => {
                  close();
                  onComposer();
                }}
                className="text-muted-foreground hover:text-foreground hover:bg-chrome-hover inline-flex h-8 items-center gap-1.5 justify-self-start rounded-full px-2.5 text-[13px] transition-colors [&_svg]:size-3.5"
              >
                <PenLine />
                换画幅或时长，再写一轮
              </button>
            </div>
          )}
        </ChipPopover>
      )}
    </>
  );
}
