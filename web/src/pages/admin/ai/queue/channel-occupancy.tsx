import { useMemo } from "react";

import type { QueueChannel } from "@/api/admin/queue/type";
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
import { channelRow } from "@/utils/admin/queue";

/**
 * 渠道占用表：每个渠道“占用 / 最大同时生成数”、等待名额数、最早等待时长和状态。
 * 能直接回答“为什么一直排队”。上限在「模型渠道」里改，这里只读；
 * 没有渠道在占用或等待时，表体显示“当前没有进行中的任务”。
 * @param channels 渠道占用
 * @param idle 整体空闲（没有任务在排队、占用）
 */
export function ChannelOccupancy({ channels, idle }: { channels: QueueChannel[]; idle: boolean }) {
  const rows = useMemo(() => channels.map(channelRow), [channels]);

  return (
    <Card data-slot="channel-occupancy" size="sm">
      <CardHeader>
        <CardTitle>渠道占用</CardTitle>
        <CardDescription className="text-xs">
          占用的名额 = 提交中 + 上游生成中。最大同时生成数在「模型渠道」里修改，这里只读。
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>渠道</TableHead>
              <TableHead>占用 / 最大同时生成数</TableHead>
              <TableHead>等待名额</TableHead>
              <TableHead>最早等待</TableHead>
              <TableHead>状态</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {idle || rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="text-muted-foreground py-6 text-center">
                  {rows.length === 0 ? "还没有渠道" : "当前没有进行中的任务"}
                </TableCell>
              </TableRow>
            ) : (
              rows.map((row) => (
                <TableRow key={row.key}>
                  <TableCell className="font-medium">{row.name}</TableCell>
                  <TableCell className="tabular-nums">{row.occupancy}</TableCell>
                  <TableCell>
                    {row.waiting > 0 ? (
                      <Tag tone="warning" className="tabular-nums">
                        {row.waiting} 个等待
                      </Tag>
                    ) : (
                      <span className="text-muted-foreground tabular-nums">0</span>
                    )}
                  </TableCell>
                  <TableCell className="text-muted-foreground tabular-nums">
                    {row.oldestWait ?? "—"}
                  </TableCell>
                  <TableCell>
                    {row.full ? <Tag tone="warning">满载</Tag> : <Tag tone="success">有空位</Tag>}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}
