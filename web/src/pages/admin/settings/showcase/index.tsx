import { useState, type ReactNode } from "react";
import { Film, ImageIcon, Minus, Plus, Upload } from "lucide-react";

import type { ShowcaseAdminItem, ShowcaseSettings } from "@/api/admin/showcase/type";
import {
  EmptyState,
  EmptyStateActions,
  EmptyStateDescription,
  EmptyStateIcon,
  EmptyStateTitle,
} from "@/components/admin-ui/empty-state";
import { Notice } from "@/components/admin-ui/notice";
import {
  PageHeader,
  PageHeaderActions,
  PageHeaderHeading,
  PageHeaderTitle,
} from "@/components/admin-ui/page-header";
import { ShowcaseLayers } from "@/components/showcase/showcase-layers";
import { ShowcaseProgress } from "@/components/showcase/showcase-progress";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { useShowcasePlayer } from "@/hooks/use-showcase-player";
import { useAdminStore } from "@/store/admin";
import { canManageInfra } from "@/utils/admin/role";
import { CLIP_SECONDS_MAX, CLIP_SECONDS_MIN } from "@/utils/showcase/rules";

import { ReadOnlyNotice } from "../../shared";
import { SettingsError } from "../settings-state";
import { ShowcaseLibrary } from "./showcase-library";
import { ShowcaseList } from "./showcase-list";
import { ShowcaseSheet } from "./showcase-sheet";
import { useShowcaseAdmin } from "./use-showcase-admin";

/** 启用的作品超过这个数，访客就很难看完一轮，提示精简 */
const RECOMMENDED_MAX = 8;

/** 抽屉的状态：关闭、上传新视频（item 为 null）、编辑某一条 */
type SheetState = { open: false } | { open: true; item: ShowcaseAdminItem | null };

/** 面板标题行：左标题，右辅助说明（设计稿 panel-head） */
function PanelHead({ title, aside }: { title: string; aside?: string }) {
  return (
    <CardHeader className="flex-row items-center justify-between gap-3 border-b py-3.5">
      <CardTitle className="text-sm">{title}</CardTitle>
      {aside && <span className="text-muted-foreground text-[12.5px] tabular-nums">{aside}</span>}
    </CardHeader>
  );
}

/**
 * 实时预览：和登录页用同一个播放器，只播启用的作品。
 * 改动保存后立刻生效，所以这里直接读服务端返回的列表和设置。
 */
function ShowcasePreview({
  items,
  settings,
}: {
  items: ReturnType<typeof useShowcaseAdmin>["playerItems"];
  settings: ShowcaseSettings;
}) {
  const player = useShowcasePlayer(items, settings.clip_seconds, settings.poster_only_on_save_data);
  const playing = settings.show_on_login && items.length > 0;

  let note = `轮播 ${items.length} 条，每条 ${settings.clip_seconds} 秒，一轮约 ${items.length * settings.clip_seconds} 秒。`;
  if (!settings.show_on_login) note = "登录页播放已关闭，显示默认渐变背景。";
  else if (items.length === 0) note = "没有启用的作品，登录页显示默认渐变背景。";

  return (
    <Card className="gap-0 py-0">
      <PanelHead title="实时预览" aside="改动保存后立即生效" />
      <CardContent className="py-4">
        <div className="bg-stage text-on-stage relative aspect-[16/10] overflow-hidden rounded-xl">
          {/* 没有作品时和登录页一样是两团柔光的渐变 */}
          <div className="absolute inset-0 bg-[radial-gradient(ellipse_70%_60%_at_70%_30%,var(--stage-glow-a),transparent_70%),radial-gradient(ellipse_60%_60%_at_20%_85%,var(--stage-glow-b),transparent_70%)]" />
          {playing && <ShowcaseLayers player={player} />}
          <div className="pointer-events-none absolute inset-0 z-10 bg-[linear-gradient(to_bottom,var(--stage-scrim-edge)_0%,transparent_24%,transparent_58%,var(--stage-scrim-edge)_100%),radial-gradient(ellipse_58%_46%_at_50%_47%,var(--stage-scrim-center),transparent_72%)]" />
          <div className="pointer-events-none absolute inset-0 z-10 flex flex-col items-center justify-center gap-2 pb-3.5">
            <span className="text-[17px] font-semibold tracking-tight [text-shadow:0_1px_12px_oklch(0_0_0/0.4)]">
              从一句话，到一部片。
            </span>
            <span className="bg-on-stage block h-4.5 w-[22%] rounded-full" />
          </div>
          <p className="text-on-stage-muted absolute inset-x-2.5 bottom-2 z-10 truncate text-[11px]">
            {playing && player.current
              ? `“${player.current.prompt}”`
              : "没有启用的作品，显示默认背景"}
          </p>
        </div>
        {playing && (
          <ShowcaseProgress player={player} tone="plain" className="mt-3 justify-between" />
        )}
        <p className="text-muted-foreground mt-2.5 text-xs leading-relaxed">{note}</p>
      </CardContent>
    </Card>
  );
}

/** 一行设置：左边标题和说明，右边控件；行与行之间一条细线（设计稿 set-row） */
function SettingRow({
  title,
  hint,
  htmlFor,
  children,
}: {
  title: string;
  hint: string;
  htmlFor?: string;
  children: ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-3 py-2.5 not-first:border-t">
      <div>
        <label htmlFor={htmlFor} className="block text-[13.5px] font-medium">
          {title}
        </label>
        <span className="text-muted-foreground text-xs">{hint}</span>
      </div>
      {children}
    </div>
  );
}

/** 轮播设置：每条时长、登录页开关、省流量开关。每改一项就立即保存 */
function SettingsPanel({
  settings,
  canWrite,
  onChange,
}: {
  settings: ShowcaseSettings;
  canWrite: boolean;
  onChange: (next: ShowcaseSettings) => void;
}) {
  const setSeconds = (seconds: number) => {
    if (seconds < CLIP_SECONDS_MIN || seconds > CLIP_SECONDS_MAX) return;
    onChange({ ...settings, clip_seconds: seconds });
  };

  return (
    <Card className="gap-0 py-0">
      <PanelHead title="轮播设置" />
      <CardContent className="py-1.5">
        <SettingRow title="每条播放时长" hint="到时间交叉溶解到下一条">
          <div className="border-border inline-flex items-center overflow-hidden rounded-lg border">
            <button
              type="button"
              aria-label="减少一秒"
              disabled={!canWrite || settings.clip_seconds <= CLIP_SECONDS_MIN}
              onClick={() => setSeconds(settings.clip_seconds - 1)}
              className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring/50 grid size-7.5 place-items-center outline-none focus-visible:ring-2 focus-visible:ring-inset disabled:opacity-40 disabled:hover:bg-transparent"
            >
              <Minus className="size-4" />
            </button>
            <output className="w-11 text-center text-[13px] tabular-nums">
              {settings.clip_seconds} 秒
            </output>
            <button
              type="button"
              aria-label="增加一秒"
              disabled={!canWrite || settings.clip_seconds >= CLIP_SECONDS_MAX}
              onClick={() => setSeconds(settings.clip_seconds + 1)}
              className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring/50 grid size-7.5 place-items-center outline-none focus-visible:ring-2 focus-visible:ring-inset disabled:opacity-40 disabled:hover:bg-transparent"
            >
              <Plus className="size-4" />
            </button>
          </div>
        </SettingRow>
        <SettingRow title="登录页播放" hint="关闭后显示默认渐变背景" htmlFor="showcase-on-login">
          <Switch
            id="showcase-on-login"
            checked={settings.show_on_login}
            disabled={!canWrite}
            onCheckedChange={(checked) => onChange({ ...settings, show_on_login: checked })}
          />
        </SettingRow>
        <SettingRow
          title="省流量时只显示封面"
          hint="浏览器开启省流量模式时不加载视频"
          htmlFor="showcase-save-data"
        >
          <Switch
            id="showcase-save-data"
            checked={settings.poster_only_on_save_data}
            disabled={!canWrite}
            onCheckedChange={(checked) =>
              onChange({ ...settings, poster_only_on_save_data: checked })
            }
          />
        </SettingRow>
      </CardContent>
    </Card>
  );
}

/**
 * 登录页展示（/admin/settings/showcase，设计稿「后台 · 登录页展示」）：
 * 左边是展示作品列表，右边是实时预览和轮播设置；顶部两个入口：上传视频、从素材库添加。
 * super_admin 可改，admin 只读。请求错误的全局提示由拦截器弹，这里不重复。
 */
export default function ShowcaseSettingsPage() {
  const role = useAdminStore((state) => state.role);
  const canWrite = canManageInfra(role);
  const admin = useShowcaseAdmin();
  const [sheet, setSheet] = useState<SheetState>({ open: false });
  const [libraryOpen, setLibraryOpen] = useState(false);
  const { view, status } = admin;
  const enabledCount = view?.items.filter((item) => item.enabled).length ?? 0;

  return (
    <div className="h-full overflow-y-auto">
      <main className="px-4 py-6 lg:px-6">
        <PageHeader>
          <PageHeaderHeading>
            <PageHeaderTitle>登录页展示</PageHeaderTitle>
          </PageHeaderHeading>
          {canWrite && (
            <PageHeaderActions>
              <Button variant="outline" onClick={() => setSheet({ open: true, item: null })}>
                <Upload />
                上传视频
              </Button>
              <Button onClick={() => setLibraryOpen(true)}>
                <ImageIcon />
                从素材库添加
              </Button>
            </PageHeaderActions>
          )}
        </PageHeader>

        {!canWrite && <ReadOnlyNotice className="mb-4" what="修改登录页展示" />}

        {status === "loading" && <Skeleton className="h-80 w-full rounded-xl" />}
        {status === "error" && <SettingsError onRetry={() => void admin.reload()} />}
        {status === "ready" && view && (
          <div className="grid items-start gap-5 lg:grid-cols-[minmax(0,1fr)_360px]">
            <Card className="gap-0 py-0">
              <PanelHead
                title="展示作品"
                aside={
                  view.items.length > 0
                    ? `${enabledCount} / ${view.items.length} 已启用`
                    : undefined
                }
              />
              {view.items.length > 0 && enabledCount === 0 && (
                <Notice tone="warning" className="m-3 mb-0">
                  没有启用的作品，登录页会显示默认渐变背景。
                </Notice>
              )}
              {enabledCount > RECOMMENDED_MAX && (
                <Notice tone="warning" className="m-3 mb-0">
                  已启用 {enabledCount} 条，超过 {RECOMMENDED_MAX} 条后访客很难看完一轮，建议精简。
                </Notice>
              )}
              {view.items.length === 0 ? (
                <EmptyState className="m-4 border-0">
                  <EmptyStateIcon>
                    <Film />
                  </EmptyStateIcon>
                  <EmptyStateTitle>还没有展示作品</EmptyStateTitle>
                  <EmptyStateDescription>登录页现在显示默认渐变背景。</EmptyStateDescription>
                  {canWrite && (
                    <EmptyStateActions>
                      <Button onClick={() => setLibraryOpen(true)}>
                        <ImageIcon />
                        从素材库添加
                      </Button>
                    </EmptyStateActions>
                  )}
                </EmptyState>
              ) : (
                <ShowcaseList
                  items={view.items}
                  busyIds={admin.busyIds}
                  canWrite={canWrite}
                  onReorder={(items) => void admin.reorder(items)}
                  onToggle={(item) => void admin.toggleEnabled(item)}
                  onEdit={(item) => setSheet({ open: true, item })}
                  onRemove={(item) => void admin.remove(item)}
                />
              )}
            </Card>

            <div className="grid gap-5 lg:sticky lg:top-6">
              <ShowcasePreview items={admin.playerItems} settings={view.settings} />
              <SettingsPanel
                settings={view.settings}
                canWrite={canWrite}
                onChange={(next) => void admin.saveSettings(next)}
              />
            </div>
          </div>
        )}

        <ShowcaseSheet
          open={sheet.open}
          item={sheet.open ? sheet.item : null}
          onClose={() => setSheet({ open: false })}
          onSaved={admin.upsert}
        />
        <ShowcaseLibrary
          open={libraryOpen}
          onClose={() => setLibraryOpen(false)}
          onAdd={admin.addFromLibrary}
        />
      </main>
    </div>
  );
}
