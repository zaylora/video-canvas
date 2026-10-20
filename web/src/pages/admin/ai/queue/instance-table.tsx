import type { QueueInfo, QueueInstance } from "@/api/admin/queue/type";
import { Notice } from "@/components/admin-ui/notice";
import { Tag } from "@/components/admin-ui/tag";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  formatAgo,
  formatStartedAt,
  instanceLoadText,
  isInstanceLost,
  lostInstances,
} from "@/utils/admin/queue";

/**
 * 实例列表：实例 ID、主机 · 版本、启动时间、心跳（在线 / 失联）、leader 标记、各队列“在途 / 生效并发”。
 * 有失联实例时在表上方显示告警条。队列只按中文标签展示。
 * @param instances 工作实例
 * @param queues 队列概况（取中文标签与顺序）
 * @param now 取到数据的时间（毫秒），相对时间据此计算
 */
export function InstanceTable({
  instances,
  queues,
  now,
}: {
  instances: QueueInstance[];
  queues: QueueInfo[];
  now: number;
}) {
  const lost = lostInstances(instances);

  return (
    <Card data-slot="instance-table" size="sm">
      <CardHeader>
        <CardTitle>实例</CardTitle>
        <CardDescription className="text-xs">并发按实例计；心跳超过 30 秒判定失联</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {lost.length > 0 && (
          <Notice tone="danger" title={`${lost.map((x) => x.hostname).join("、")} 已失联。`}>
            它正在运行的任务会被其他实例救回并重新执行，可能需要等待一段时间。
          </Notice>
        )}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>实例 ID</TableHead>
              <TableHead>主机 · 版本</TableHead>
              <TableHead>启动</TableHead>
              <TableHead>心跳</TableHead>
              <TableHead />
              <TableHead>在途 / 生效并发</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {instances.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="text-muted-foreground py-6 text-center">
                  没有工作实例
                </TableCell>
              </TableRow>
            ) : (
              instances.map((instance) => {
                const gone = isInstanceLost(instance);
                const ago = formatAgo(instance.heartbeat_at, now);
                return (
                  <TableRow key={instance.id}>
                    <TableCell className="font-mono text-xs">{instance.id}</TableCell>
                    <TableCell>
                      {instance.hostname} · {instance.version}
                    </TableCell>
                    <TableCell className="text-muted-foreground tabular-nums">
                      {formatStartedAt(instance.started_at, now)}
                    </TableCell>
                    <TableCell>
                      <Tag tone={gone ? "danger" : "success"} className="tabular-nums">
                        {gone ? "失联" : "在线"}
                        {ago && ` · ${ago}`}
                      </Tag>
                    </TableCell>
                    <TableCell>{instance.is_leader && <Tag tone="info">leader</Tag>}</TableCell>
                    <TableCell className="text-xs tabular-nums">
                      {queues
                        .map(
                          (queue) =>
                            `${queue.label} ${instanceLoadText(instance.stats[queue.name], !gone)}`,
                        )
                        .join(" · ")}
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}
