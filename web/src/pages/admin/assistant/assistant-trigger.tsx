import { useEffect } from "react";
import { Sparkles } from "lucide-react";

import { ChromeButton, ChromePill, Kbd } from "@/components/canvas/chrome/chrome";
import { MOD } from "@/components/canvas-editor/keys";
import { ASSISTANT_NAME } from "@/constants/agent";
import { useAdminStore } from "@/store/admin";

/** 快捷键 Ctrl/⌘ + J 是不是按下了：不带 Shift、Alt */
export function isAssistantHotkey(event: {
  key: string;
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
}): boolean {
  return (
    (event.ctrlKey || event.metaKey) &&
    !event.shiftKey &&
    !event.altKey &&
    event.key.toLowerCase() === "j"
  );
}

/** 全后台的 Ctrl/⌘ + J：展开或收起助手抽屉；浏览器自带的「下载」快捷键被它占用，所以要阻止默认行为 */
export function useAssistantHotkey(): void {
  const toggle = useAdminStore((s) => s.toggleAssistant);
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (!isAssistantHotkey(event)) return;
      event.preventDefault();
      toggle();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [toggle]);
}

/** 后台顶栏的「助手」入口：清透胶囊，带快捷键提示；展开时是按下状态 */
export function AssistantTrigger() {
  const open = useAdminStore((s) => s.assistantOpen);
  const toggle = useAdminStore((s) => s.toggleAssistant);
  return (
    <ChromePill>
      <ChromeButton
        aria-pressed={open}
        aria-label={ASSISTANT_NAME}
        active={open}
        className={open ? "bg-chrome-hover text-foreground" : undefined}
        onClick={toggle}
      >
        <Sparkles aria-hidden />
        <span className="text-xs font-medium">{ASSISTANT_NAME}</span>
        <Kbd>{MOD === "⌘" ? "⌘J" : "Ctrl J"}</Kbd>
      </ChromeButton>
    </ChromePill>
  );
}
