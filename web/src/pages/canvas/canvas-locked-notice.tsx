import { useNavigate } from "react-router";

import { Button } from "@/components/ui/button";

/**
 * 这张画布已在另一个标签页打开：这里先不能编辑，免得两个标签页互相覆盖本地草稿。
 * 另一个标签页关闭后，这里会自动接管并打开画布，不需要手动刷新。
 */
export function CanvasLockedNotice() {
  const navigate = useNavigate();
  return (
    <div className="grid min-h-svh place-items-center px-4">
      <div className="max-w-sm text-center" role="status">
        <h1 className="text-lg font-semibold">这张画布已在另一个标签页打开</h1>
        <p className="text-muted-foreground mt-2 text-sm">
          为避免两个标签页互相覆盖改动，这里暂时不能编辑。关闭另一个标签页后，这里会自动打开画布。
        </p>
        <Button variant="outline" className="mt-5" onClick={() => navigate("/canvases")}>
          返回画布列表
        </Button>
      </div>
    </div>
  );
}
