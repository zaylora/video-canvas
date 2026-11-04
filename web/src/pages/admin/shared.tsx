import { ShieldAlert } from "lucide-react";
import { Link } from "react-router";

import { Notice } from "@/components/admin-ui/notice";
import { Button } from "@/components/ui/button";

/**
 * 没有管理权限时的整页提示。角色变更后端最多延迟一个缓存周期，所以给“重试”。
 * @param onRetry 点重试时重新取角色；不传则不显示按钮
 */
export function ForbiddenView({ onRetry }: { onRetry?: () => void }) {
  return (
    <main className="grid min-h-svh place-items-center p-6">
      <div className="flex max-w-sm flex-col items-center gap-3 text-center">
        <ShieldAlert className="text-destructive size-10" />
        <h1 className="text-xl font-semibold">403 没有管理权限</h1>
        <p className="text-muted-foreground text-sm">请联系运维开通权限</p>
        <div className="flex items-center gap-3">
          {onRetry && (
            <Button variant="outline" size="sm" onClick={onRetry}>
              重试
            </Button>
          )}
          <Link to="/" className="text-primary text-sm hover:underline">
            返回我的画布
          </Link>
        </div>
      </div>
    </main>
  );
}

/** 只读提示：admin 看插件页 / 渠道页时显示，说明为什么没有写操作按钮 */
export function ReadOnlyNotice({ what, className }: { what: string; className?: string }) {
  return (
    <Notice tone="info" title="只读" className={className}>
      {what}需要 super_admin 权限
    </Notice>
  );
}
