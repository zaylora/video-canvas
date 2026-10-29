import { Bot } from "lucide-react";

import { VendorAvatar } from "@/components/admin-ui/vendor-avatar";
import { vendorOf } from "@/constants/vendors";
import { cn } from "@/lib/utils";

/**
 * 模型的厂商 logo：配了厂商用 VendorAvatar，没配回退 Bot 图标（不用首字头像，在工具条、菜单里太抢眼）。
 * @param vendor 厂商 slug（如 google、openai）
 * @param name 模型名，VendorAvatar 的无障碍名
 */
export function ModelLogo({
  vendor,
  name,
  className,
}: {
  vendor?: string;
  name: string;
  className?: string;
}) {
  if (!vendorOf(vendor)) return <Bot className="size-4 shrink-0" />;
  return (
    <VendorAvatar
      vendor={vendor}
      name={name}
      // 按钮常给里面的 svg 统一定尺寸（[&_svg]:size-4），会把 logo 撑满方块、四角被圆角切掉；这里强制保持内缩
      className={cn("shrink-0 border-0 shadow-none [&_svg]:size-[68%]!", className)}
    />
  );
}
