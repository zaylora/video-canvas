import type { ComponentProps } from "react";

import { InitialAvatar } from "@/components/admin-ui/initial-avatar";
import { vendorOf } from "@/constants/vendors";
import { cn } from "@/lib/utils";

/**
 * 模型头像：有 vendor 且在内置清单里显示厂商 logo（浅色底圆角方块，亮暗主题下观感一致），
 * 否则回退成 InitialAvatar 首字头像。尺寸与字号用 className 调（默认 size-9）。
 * @param vendor 厂商 slug（模型配置的 vendor 字段）
 * @param name 模型名，首字头像用
 * @param seed 决定首字头像颜色的字符串，一般用模型 key
 */
function VendorAvatar({
  vendor,
  name,
  seed,
  className,
  ...props
}: ComponentProps<"span"> & { vendor?: string; name: string; seed?: string }) {
  const entry = vendorOf(vendor);
  if (!entry) return <InitialAvatar name={name} seed={seed} className={className} {...props} />;
  const { Icon } = entry;
  return (
    <span
      data-slot="vendor-avatar"
      role="img"
      aria-label={entry.name}
      title={entry.name}
      className={cn(
        "grid size-9 shrink-0 place-items-center rounded-lg border border-black/5 bg-white text-neutral-900 shadow-sm",
        className,
      )}
      {...props}
    >
      <Icon size="68%" />
    </span>
  );
}

export { VendorAvatar };
