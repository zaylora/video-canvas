import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";

/**
 * 删除组的确认框：组里的节点会一起被删，而节点里有生成结果和积分花费，所以不能一键就清掉。
 * 虽然 ⌘Z 能整体恢复，仍按设计稿 6.10 保留确认。
 */
export function GroupDeleteDialog({
  target,
  onConfirm,
  onCancel,
}: {
  /** 要删的组；null 表示不弹 */
  target: { name: string; memberCount: number } | null;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <AlertDialog open={target !== null} onOpenChange={(open) => !open && onCancel()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>删除「{target?.name}」？</AlertDialogTitle>
          <AlertDialogDescription>
            {target?.memberCount
              ? `里面的 ${target.memberCount} 个节点会一起被删除，相关连线也会断开。可以用撤销恢复。`
              : "这个组里没有节点，只会删除组本身。"}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction variant="destructive" autoFocus onClick={onConfirm}>
            {target?.memberCount ? "删除组和节点" : "删除组"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
