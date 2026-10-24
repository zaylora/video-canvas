import type { ReactNode } from "react";
import { ImageOff, UserRoundPen, UserRoundX, Unlink, type LucideIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";
import { summarizeImportReport, type ImportReport } from "@/utils/canvas/import-flow";

/**
 * 导入结果弹窗：画布已经导入成功，这里列出需要用户留意的项。
 * 只有「打开画布」会跳转，其他方式关闭就留在列表页。
 * @param report 导入报告，null 表示不显示
 * @param onClose 关闭
 * @param onOpen 打开新画布
 */
export function ImportResultDialog({
  report,
  onClose,
  onOpen,
}: {
  report: ImportReport | null;
  onClose: () => void;
  onOpen: () => void;
}) {
  return (
    <Dialog open={report !== null} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="sm:max-w-md">
        {report && <ReportBody report={report} onOpen={onOpen} />}
      </DialogContent>
    </Dialog>
  );
}

function ReportBody({ report, onOpen }: { report: ImportReport; onOpen: () => void }) {
  const summary = summarizeImportReport(report);

  return (
    <>
      <DialogHeader>
        <DialogTitle>导入完成，有 {summary.total} 项需要注意</DialogTitle>
        <DialogDescription>新画布已创建，下面这些地方可能需要你补一补。</DialogDescription>
      </DialogHeader>
      <div className="grid max-h-[50vh] gap-2.5 overflow-y-auto">
        {summary.missing > 0 && (
          <Section icon={ImageOff} tone="warn" title={`${summary.missing} 个素材找不到，已留空`}>
            {report.assets.missing.map((item) => (
              <li key={`${item.nodeId}-${item.fileName}`}>
                节点 <code>{item.nodeId}</code> · {item.fileName}
              </li>
            ))}
          </Section>
        )}
        {summary.renamed > 0 && (
          <Section
            icon={UserRoundPen}
            tone="info"
            title={`${summary.renamed} 张角色卡重名，已自动改名`}
          >
            {report.characters.renamed.map((item) => (
              <li key={`${item.from}-${item.to}`}>
                {item.from} → {item.to}
              </li>
            ))}
          </Section>
        )}
        {summary.invalid > 0 && (
          <Section
            icon={UserRoundX}
            tone="warn"
            title={`${summary.invalid} 张角色卡主图缺失，没有创建`}
          >
            {report.characters.invalid.map((item) => (
              <li key={`${item.nodeId}-${item.name}`}>
                节点 <code>{item.nodeId}</code> · {item.name}（显示为「已失效」）
              </li>
            ))}
          </Section>
        )}
        {summary.edgesDropped > 0 && (
          <Section
            icon={Unlink}
            tone="info"
            title={`${summary.edgesDropped} 根连线指向不存在的节点，已丢弃`}
          />
        )}
      </div>
      <DialogFooter>
        <Button onClick={onOpen}>打开画布</Button>
      </DialogFooter>
    </>
  );
}

/** 一类需要注意的项：警告（warn）用 status-warning 主题色打底，提示（info）用中性底色 */
function Section({
  icon: Icon,
  tone,
  title,
  children,
}: {
  icon: LucideIcon;
  tone: "warn" | "info";
  title: string;
  children?: ReactNode;
}) {
  return (
    <div
      className={cn(
        "text-foreground flex gap-2.5 rounded-lg p-3",
        tone === "warn" ? "bg-status-warning/10" : "bg-muted/60",
      )}
    >
      {/* 浅色模式下 status-warning 作正文色对比度不够，只给图标用，文字保持前景色 */}
      <Icon className={cn("mt-0.5 size-4 shrink-0", tone === "warn" && "text-status-warning")} />
      <div className="min-w-0 flex-1">
        <p className="font-medium">{title}</p>
        {children && (
          <ul className="text-foreground/80 mt-1 list-disc space-y-0.5 pl-4 text-[13px] break-all">
            {children}
          </ul>
        )}
      </div>
    </div>
  );
}
