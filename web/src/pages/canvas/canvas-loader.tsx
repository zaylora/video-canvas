import { FocusLoader } from "@/components/focus-loader";

/**
 * 进入画布前的加载层（设计稿原型 A「聚焦显影」）：白色取景框 logo + 产品名，
 * 画布数据到了（ready）后退场。动画和停留时长都在 FocusLoader 里，和进后台共用。
 */
export function CanvasLoader({
  title,
  ready,
  onOpen,
  onDone,
}: {
  title?: string;
  ready: boolean;
  onOpen: () => void;
  onDone: () => void;
}) {
  return (
    <FocusLoader
      icon={
        <svg viewBox="0 0 16 16" fill="none" className="size-10" aria-hidden>
          <rect x="2.5" y="4" width="8" height="8" rx="2" stroke="currentColor" strokeWidth="1.6" />
          <path d="M10.5 7.2 13.5 5.5v5l-3-1.7" fill="currentColor" />
        </svg>
      }
      title="Video Canvas"
      subtitle={title ? `正在打开「${title}」` : "正在打开画布"}
      label={title ? `正在打开画布「${title}」` : "正在打开画布"}
      ready={ready}
      onOpen={onOpen}
      onDone={onDone}
    />
  );
}
