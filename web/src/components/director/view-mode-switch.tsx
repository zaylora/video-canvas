import { useEffect, useRef, useState } from "react";
import { Camera, Check, ChevronDown, Compass } from "lucide-react";

import { cn } from "@/lib/utils";
import { useDirectorStore } from "@/store/director";
import type { DirectorCamera } from "@/types";
import { activeCameraOf } from "@/utils/director/camera-view";

/** 没有场景时的机位列表；用常量，选择器每次返回同一个引用 */
const NO_CAMERAS: DirectorCamera[] = [];

/** 菜单项的标记：编辑器的快捷键处理看到事件落在带这个标记的元素上，就交给菜单自己处理（方向键、Esc） */
export const MENU_ATTR = "data-director-menu";

/**
 * 视口底部的视角切换：导演视角（自由环绕），或进入某一台机位里看（机位视角）。
 * 菜单里先是导演视角，然后有多少台机位就列多少项，点哪台就进哪台；场景里没有机位时提示先添加。
 * 编辑器里不用会 Portal 到 body 的下拉组件（会跳出深色范围），所以菜单是自己画的。
 */
export function ViewModeSwitch() {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const cameras = useDirectorStore((state) => state.scene?.cameras ?? NO_CAMERAS);
  /** 正在看的机位 id；导演视角（或没有可看的机位）是 null */
  const viewedId = useDirectorStore((state) => activeCameraOf(state.scene, state.viewMode));
  const viewedName = cameras.find((c) => c.id === viewedId)?.name;
  const inCamera = viewedId !== null;

  // 点菜单外面就收起
  useEffect(() => {
    if (!open) return;
    const onDown = (event: PointerEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) setOpen(false);
    };
    window.addEventListener("pointerdown", onDown, true);
    return () => window.removeEventListener("pointerdown", onDown, true);
  }, [open]);

  // 展开后焦点落到当前项，键盘可以直接上下选
  useEffect(() => {
    if (open) rootRef.current?.querySelector<HTMLElement>('[aria-checked="true"]')?.focus();
  }, [open]);

  const choose = (cameraId: string | null) => {
    const store = useDirectorStore.getState();
    if (cameraId === null) store.setViewMode("director");
    else store.enterCameraView(cameraId);
    setOpen(false);
    triggerRef.current?.focus();
  };

  const onMenuKey = (event: React.KeyboardEvent) => {
    if (event.key === "Escape") {
      event.stopPropagation();
      setOpen(false);
      triggerRef.current?.focus();
      return;
    }
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
    event.preventDefault();
    const items = [
      ...(rootRef.current?.querySelectorAll<HTMLElement>('[role="menuitemradio"]') ?? []),
    ];
    const index = items.indexOf(document.activeElement as HTMLElement);
    const next = event.key === "ArrowDown" ? index + 1 : index - 1;
    items[(next + items.length) % items.length]?.focus();
  };

  return (
    <div
      ref={rootRef}
      className="absolute bottom-3 left-1/2 z-10 -translate-x-1/2"
      onKeyDown={open ? onMenuKey : undefined}
    >
      {open && (
        <div
          role="menu"
          aria-label="视角"
          className="bg-card border-border absolute bottom-full left-1/2 mb-2 max-h-72 w-52 -translate-x-1/2 overflow-y-auto rounded-xl border p-1.5 shadow-lg"
        >
          <MenuItem
            icon={<Compass />}
            label="导演视角"
            checked={!inCamera}
            onClick={() => choose(null)}
          />
          {cameras.length === 0 && (
            <p className="text-muted-foreground px-2.5 py-1.5 text-xs">先添加一台机位</p>
          )}
          {cameras.map((camera) => (
            <MenuItem
              key={camera.id}
              icon={<Camera />}
              label={camera.name}
              checked={camera.id === viewedId}
              onClick={() => choose(camera.id)}
            />
          ))}
        </div>
      )}
      <button
        ref={triggerRef}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        title="切换视角"
        onClick={() => setOpen((value) => !value)}
        className="bg-card/90 border-border text-foreground hover:bg-muted focus-visible:ring-ring inline-flex h-9 items-center gap-2 rounded-xl border px-3 text-xs font-semibold shadow-lg backdrop-blur-sm outline-none focus-visible:ring-2 [&_svg]:size-4"
      >
        {inCamera ? (
          <Camera className="text-muted-foreground" />
        ) : (
          <Compass className="text-muted-foreground" />
        )}
        {inCamera ? `机位视角 · ${viewedName ?? ""}` : "导演视角"}
        <ChevronDown
          className={cn(
            "text-muted-foreground size-3.5! transition-transform",
            open && "rotate-180",
          )}
        />
      </button>
    </div>
  );
}

function MenuItem({
  icon,
  label,
  checked,
  onClick,
}: {
  icon?: React.ReactNode;
  label: string;
  checked: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="menuitemradio"
      aria-checked={checked}
      {...{ [MENU_ATTR]: "" }}
      onClick={onClick}
      className={cn(
        "text-foreground hover:bg-muted focus-visible:bg-muted flex h-9 w-full items-center gap-2.5 rounded-lg px-2.5 text-left text-xs font-medium outline-none [&_svg]:size-4",
        checked && "bg-muted",
      )}
    >
      {icon && <span className="text-muted-foreground flex">{icon}</span>}
      <span className="truncate">{label}</span>
      {checked && <Check className="ml-auto shrink-0" />}
    </button>
  );
}
