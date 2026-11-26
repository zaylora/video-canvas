import { useId, useState } from "react";
import { ChevronDown } from "lucide-react";

import { ChromeButton } from "@/components/canvas/chrome/chrome";
import { Popover, PopoverContent, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import { Slider } from "@/components/ui/slider";

export const DEFAULT_SHARED_REFRESH_SECONDS = 15;
export const MAX_SHARED_REFRESH_SECONDS = 60;

export function SharedRefreshControl({
  enabled,
  seconds,
  onEnabledChange,
  onIntervalChange,
  onRefresh,
}: {
  enabled: boolean;
  seconds: number;
  onEnabledChange: (enabled: boolean) => void;
  onIntervalChange: (seconds: number) => void;
  onRefresh: () => void;
}) {
  const group = useId();
  const [draftSeconds, setDraftSeconds] = useState(seconds);

  return (
    <div className="flex shrink-0 items-center">
      <ChromeButton className="rounded-r-sm" onClick={onRefresh}>
        刷新
      </ChromeButton>
      <Popover>
        <PopoverTrigger
          render={
            <ChromeButton
              aria-label="自动刷新设置"
              title={enabled ? `自动刷新 · ${seconds} 秒` : "不刷新"}
              className="rounded-l-sm data-popup-open:bg-chrome-hover"
            >
              <ChevronDown />
            </ChromeButton>
          }
        />
        <PopoverContent align="end" sideOffset={10} className="w-56 gap-3 rounded-xl p-3">
          <PopoverTitle>刷新设置</PopoverTitle>
          <fieldset className="grid gap-1">
            <legend className="sr-only">刷新方式</legend>
            <label className="hover:bg-chrome-hover flex min-h-8 cursor-pointer items-center gap-2 rounded-md px-2">
              <input
                type="radio"
                name={group}
                value="disabled"
                checked={!enabled}
                onChange={() => {
                  setDraftSeconds(seconds);
                  onEnabledChange(false);
                }}
                className="accent-primary"
              />
              不刷新
            </label>
            <label className="hover:bg-chrome-hover flex min-h-8 cursor-pointer items-center gap-2 rounded-md px-2">
              <input
                type="radio"
                name={group}
                value="automatic"
                checked={enabled}
                onChange={() => onEnabledChange(true)}
                className="accent-primary"
              />
              自动刷新
            </label>
          </fieldset>
          {enabled && (
            <label className="flex items-center gap-3 px-2 py-1">
              <span className="sr-only">自动刷新间隔（秒）</span>
              <Slider
                min={1}
                max={MAX_SHARED_REFRESH_SECONDS}
                value={[draftSeconds]}
                onValueChange={(value) =>
                  setDraftSeconds(typeof value === "number" ? value : value[0])
                }
                onValueCommitted={(value) =>
                  onIntervalChange(typeof value === "number" ? value : value[0])
                }
              />
              <span
                aria-hidden="true"
                className="text-muted-foreground w-11 shrink-0 text-right tabular-nums"
              >
                {draftSeconds} 秒
              </span>
            </label>
          )}
        </PopoverContent>
      </Popover>
    </div>
  );
}
