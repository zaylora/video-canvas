import {
  Clapperboard,
  Film,
  LayoutGrid,
  Moon,
  PanelRight,
  ScanSearch,
  ScrollText,
  Sun,
  Users,
  type LucideIcon,
} from "lucide-react";

import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "@/components/ui/command";
import { useSettingsStore } from "@/store/settings";
import { jumpStageFor, STAGES, type WorkspaceStage } from "@/utils/drama/stages";

import { useWorkspace } from "./context";

const STAGE_ICON: Record<WorkspaceStage, LucideIcon> = {
  overview: LayoutGrid,
  script: ScrollText,
  assets: Users,
  studio: Clapperboard,
  final: Film,
};

/**
 * ⌘K 命令面板：跳到某道工序、某一集，或执行常用操作（资产拆解、收起右栏、切换明暗）。
 * 搜索由 cmdk 完成；选中后先关面板再执行，避免面板关闭动画和页面切换抢焦点。
 */
export function CommandPalette({
  open,
  onOpenChange,
  onGo,
  onBreakdownOpen,
  onToggleSide,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onGo: (stage: WorkspaceStage, ep?: number) => void;
  onBreakdownOpen: () => void;
  onToggleSide: () => void;
}) {
  const { drama } = useWorkspace();
  const theme = useSettingsStore((state) => state.theme);
  const updateSettings = useSettingsStore((state) => state.updateSettings);
  const dark =
    theme === "dark" ||
    (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);

  const run = (action: () => void) => () => {
    onOpenChange(false);
    action();
  };

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title="命令面板"
      description="跳转到工序、分集，或执行常用操作"
    >
      <Command>
        <CommandInput placeholder="跳转到工序、分集，或执行操作…" />
        <CommandList>
          <CommandEmpty>没有找到匹配的内容</CommandEmpty>
          <CommandGroup heading="工序">
            {STAGES.map(({ key, label }, index) => {
              const Icon = STAGE_ICON[key];
              return (
                <CommandItem key={key} value={`工序 ${label}`} onSelect={run(() => onGo(key))}>
                  <Icon />
                  {label}
                  <CommandShortcut>{index + 1}</CommandShortcut>
                </CommandItem>
              );
            })}
          </CommandGroup>
          <CommandGroup heading="操作">
            <CommandItem value="操作 资产拆解" onSelect={run(onBreakdownOpen)}>
              <ScanSearch />
              资产拆解
            </CommandItem>
            <CommandItem value="操作 右栏 助手" onSelect={run(onToggleSide)}>
              <PanelRight />
              展开 / 收起右栏（短剧助手）
            </CommandItem>
            <CommandItem
              value="操作 切换 明暗 主题"
              onSelect={run(() => updateSettings("theme", dark ? "light" : "dark"))}
            >
              {dark ? <Sun /> : <Moon />}
              切换到{dark ? "浅色" : "深色"}
            </CommandItem>
          </CommandGroup>
          <CommandGroup heading="分集">
            {drama.episodes.map((episode) => (
              <CommandItem
                key={episode.no}
                value={`分集 第${episode.no}集 ${episode.title}`}
                onSelect={run(() => onGo(jumpStageFor(episode), episode.no))}
              >
                <Film />第 {episode.no} 集 · {episode.title}
              </CommandItem>
            ))}
          </CommandGroup>
        </CommandList>
      </Command>
    </CommandDialog>
  );
}
