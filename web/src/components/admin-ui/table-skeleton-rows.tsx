import { Skeleton } from "@/components/ui/skeleton";
import { TableCell, TableRow } from "@/components/ui/table";

/** 每列骨架条的宽度轮换，避免一整列等宽像个色块 */
const WIDTHS = ["w-28", "w-20", "w-24", "w-16", "w-20"] as const;

/**
 * 表格加载中的骨架行：表头、工具条照常显示，只把表格体换成灰条，行高与真实行接近（h-14）。
 * 放在 <TableBody> 里；列数传表头的列数，操作列也算。
 * @param columns 列数
 * @param rows 骨架行数，默认 3
 */
function TableSkeletonRows({ columns, rows = 3 }: { columns: number; rows?: number }) {
  return Array.from({ length: rows }, (_, row) => (
    <TableRow key={row} data-slot="table-skeleton-row" className="hover:bg-transparent" aria-hidden>
      {Array.from({ length: columns }, (_, col) => (
        <TableCell key={col} className="h-14 px-3">
          <Skeleton className={`h-4 ${WIDTHS[(row + col) % WIDTHS.length]}`} />
        </TableCell>
      ))}
    </TableRow>
  ));
}

export { TableSkeletonRows };
