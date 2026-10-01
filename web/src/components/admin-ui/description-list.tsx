import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * 详情信息格（字段名在上、值在下）：
 * <DescriptionList><DescriptionItem><DescriptionTerm /><DescriptionDetails /></DescriptionItem></DescriptionList>
 * 列数用 className 控制，默认 2 列、md 3 列、2xl 5 列。
 */
function DescriptionList({ className, ...props }: ComponentProps<"dl">) {
  return (
    <dl
      data-slot="description-list"
      className={cn(
        "grid grid-cols-2 gap-x-6 gap-y-4 text-sm md:grid-cols-3 2xl:grid-cols-5",
        className,
      )}
      {...props}
    />
  );
}

function DescriptionItem({ className, ...props }: ComponentProps<"div">) {
  return <div data-slot="description-item" className={cn("min-w-0", className)} {...props} />;
}

function DescriptionTerm({ className, ...props }: ComponentProps<"dt">) {
  return (
    <dt
      data-slot="description-term"
      className={cn("text-muted-foreground text-xs", className)}
      {...props}
    />
  );
}

function DescriptionDetails({ className, ...props }: ComponentProps<"dd">) {
  return (
    <dd
      data-slot="description-details"
      className={cn("mt-1 flex flex-wrap items-center gap-1.5 font-medium", className)}
      {...props}
    />
  );
}

export { DescriptionDetails, DescriptionItem, DescriptionList, DescriptionTerm };
