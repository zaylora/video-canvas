import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";

/**
 * 文本节点的放大预览：居中的只读弹窗，显示节点全文，可滚动、可选中复制，Esc 或点遮罩关闭。
 * 弹窗是 Portal，React 事件仍会冒泡回画布节点，所以点击和按下要拦住，免得选中、拖动了节点。
 * @param props.open 是否打开
 * @param props.onOpenChange 打开状态变化（关闭时为 false）
 * @param props.title 弹窗标题，用节点名
 * @param props.text 节点全文
 */
export function TextPreviewDialog({
  open,
  onOpenChange,
  title,
  text,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  text: string;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="sm:max-w-2xl"
        onClick={(event) => event.stopPropagation()}
        onPointerDown={(event) => event.stopPropagation()}
      >
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <div className="max-h-[70vh] overflow-y-auto text-sm leading-7 whitespace-pre-wrap select-text">
          {text}
        </div>
      </DialogContent>
    </Dialog>
  );
}
