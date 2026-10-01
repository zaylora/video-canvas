import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";

const PAGE_SIZES = [10, 20, 30, 40, 50] as const;

/** 页码按钮：最多 5 个，两端保留首尾页，中间用省略号（与 shadcn-admin 的 getPageNumbers 一致） */
function getPageNumbers(currentPage: number, totalPages: number): (number | "...")[] {
  const maxVisible = 5;
  if (totalPages <= maxVisible) return Array.from({ length: totalPages }, (_, i) => i + 1);
  if (currentPage <= 3) return [1, 2, 3, 4, "...", totalPages];
  if (currentPage >= totalPages - 2)
    return [1, "...", totalPages - 3, totalPages - 2, totalPages - 1, totalPages];
  return [1, "...", currentPage - 1, currentPage, currentPage + 1, "...", totalPages];
}

/**
 * 表格分页（shadcn-admin 的 DataTablePagination）：左侧每页条数，右侧“第 x / y 页”、
 * 首页 / 上一页 / 页码 / 下一页 / 末页。页码从 1 开始。
 */
function DataTablePagination({
  page,
  pageSize,
  total,
  onPageChange,
  onPageSizeChange,
  className,
}: {
  page: number;
  pageSize: number;
  total: number;
  onPageChange: (page: number) => void;
  onPageSizeChange: (size: number) => void;
  className?: string;
}) {
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  const pageNumbers = getPageNumbers(page, totalPages);
  const canPrev = page > 1;
  const canNext = page < totalPages;

  return (
    <div
      data-slot="data-table-pagination"
      className={cn(
        "flex flex-col-reverse items-center justify-between gap-4 px-2 sm:flex-row",
        className,
      )}
    >
      <div className="flex w-full items-center justify-between sm:w-auto sm:justify-start sm:gap-6">
        <div className="flex items-center gap-2">
          <Select
            value={String(pageSize)}
            onValueChange={(value) => value && onPageSizeChange(Number(value))}
          >
            <SelectTrigger className="h-8 w-[4.5rem]" aria-label="每页条数">
              <SelectValue />
            </SelectTrigger>
            <SelectContent side="top" alignItemWithTrigger={false}>
              {PAGE_SIZES.map((size) => (
                <SelectItem key={size} value={String(size)}>
                  {size}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="hidden text-sm font-medium sm:block">每页条数</p>
        </div>
        <span className="text-muted-foreground text-sm tabular-nums">共 {total} 条</span>
      </div>

      <div className="flex items-center gap-4 lg:gap-8">
        <div className="flex min-w-24 items-center justify-center text-sm font-medium tabular-nums">
          第 {page} / {totalPages} 页
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            className="size-8 p-0 max-md:hidden"
            onClick={() => onPageChange(1)}
            disabled={!canPrev}
          >
            <span className="sr-only">第一页</span>
            <ChevronsLeft />
          </Button>
          <Button
            variant="outline"
            className="size-8 p-0"
            onClick={() => onPageChange(page - 1)}
            disabled={!canPrev}
          >
            <span className="sr-only">上一页</span>
            <ChevronLeft />
          </Button>
          {pageNumbers.map((number, index) =>
            number === "..." ? (
              <span
                key={`dots-${index}`}
                className="text-muted-foreground px-1 text-sm max-sm:hidden"
              >
                …
              </span>
            ) : (
              <Button
                key={number}
                variant={page === number ? "default" : "outline"}
                className="h-8 min-w-8 px-2 max-sm:hidden"
                onClick={() => onPageChange(number)}
              >
                <span className="sr-only">第 {number} 页</span>
                {number}
              </Button>
            ),
          )}
          <Button
            variant="outline"
            className="size-8 p-0"
            onClick={() => onPageChange(page + 1)}
            disabled={!canNext}
          >
            <span className="sr-only">下一页</span>
            <ChevronRight />
          </Button>
          <Button
            variant="outline"
            className="size-8 p-0 max-md:hidden"
            onClick={() => onPageChange(totalPages)}
            disabled={!canNext}
          >
            <span className="sr-only">最后一页</span>
            <ChevronsRight />
          </Button>
        </div>
      </div>
    </div>
  );
}

export { DataTablePagination, getPageNumbers, PAGE_SIZES };
