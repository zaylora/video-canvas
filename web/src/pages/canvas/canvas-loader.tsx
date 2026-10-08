import { Logo } from "@/components/brand/logo";
import { FocusLoader } from "@/components/focus-loader";

/**
 * 进入画布前的加载层（设计稿原型 A「聚焦显影」）：连镜 Logo + 产品名，
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
      icon={<Logo size={44} />}
      title="连镜"
      subtitle={title ? `正在打开「${title}」` : "正在打开画布"}
      label={title ? `正在打开画布「${title}」` : "正在打开画布"}
      ready={ready}
      onOpen={onOpen}
      onDone={onDone}
    />
  );
}
