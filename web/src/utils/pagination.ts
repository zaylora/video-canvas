/** 页码按钮：最多 5 个，两端保留首尾页，中间用省略号（与 shadcn-admin 的 getPageNumbers 一致） */
export function getPageNumbers(currentPage: number, totalPages: number): (number | "...")[] {
  const maxVisible = 5;
  if (totalPages <= maxVisible) return Array.from({ length: totalPages }, (_, i) => i + 1);
  if (currentPage <= 3) return [1, 2, 3, 4, "...", totalPages];
  if (currentPage >= totalPages - 2)
    return [1, "...", totalPages - 3, totalPages - 2, totalPages - 1, totalPages];
  return [1, "...", currentPage - 1, currentPage, currentPage + 1, "...", totalPages];
}
