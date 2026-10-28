import { useState } from "react";
import { Combine, Download, Loader2, TriangleAlert } from "lucide-react";

import type { AssetDto } from "@/api/asset/type";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { downloadMedia } from "@/utils/canvas/download";

/** 「x:y」比例转成 CSS aspect-ratio；认不出来用 9:16 */
const aspectOf = (ratio: string) => (/^\d+:\d+$/.test(ratio) ? ratio.replace(":", "/") : "9/16");

/** 成片下载用的文件名 */
export const finalFileName = (title: string, no: number) => `${title || "短剧"}-第${no}集.mp4`;

type FinalVideoProps = {
  asset: AssetDto;
  /** 项目比例，决定播放器画幅 */
  ratio: string;
  /** 成片之后有片段改过 */
  stale: boolean;
  title: string;
  no: number;
  /** 重新合成；不传就不显示按钮（例如已有任务在跑） */
  onRecompose?: () => void;
  /** 正在合成 / 提交中 */
  composing?: boolean;
  /** 不能重新合成的原因，写在按钮的 title 上 */
  recomposeReason?: string | null;
  className?: string;
};

/**
 * 成片：播放、下载，过期时提示「成片已过期，可重新合成」并给出重新合成按钮。
 * 分集卡片的弹窗和片段工作区的右预览共用。
 */
export function FinalVideo({
  asset,
  ratio,
  stale,
  title,
  no,
  onRecompose,
  composing,
  recomposeReason,
  className,
}: FinalVideoProps) {
  const [saving, setSaving] = useState(false);
  const save = async () => {
    setSaving(true);
    try {
      await downloadMedia(asset.url, finalFileName(title, no));
    } finally {
      setSaving(false);
    }
  };
  return (
    <div className={className}>
      <div
        style={{ aspectRatio: aspectOf(ratio) }}
        className="bg-muted mx-auto grid max-h-[420px] w-full place-items-center overflow-hidden rounded-[14px]"
      >
        <video
          key={asset.id}
          src={asset.url}
          controls
          playsInline
          preload="metadata"
          className="size-full bg-black object-contain"
        />
      </div>
      {stale && (
        <p
          role="status"
          className="text-status-warning mt-2 flex items-start gap-1.5 text-xs leading-5"
        >
          <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />
          成片已过期，可重新合成
        </p>
      )}
      <div className="mt-2 flex items-center gap-1.5">
        <Button variant="outline" size="sm" disabled={saving} onClick={() => void save()}>
          {saving ? <Loader2 className="animate-spin" /> : <Download />}
          下载
        </Button>
        {onRecompose && (
          <Button
            variant={stale ? "default" : "ghost"}
            size="sm"
            disabled={!!recomposeReason || composing}
            title={recomposeReason ?? "按当前采用的片段重新拼接，不扣积分"}
            onClick={onRecompose}
          >
            {composing ? <Loader2 className="animate-spin" /> : <Combine />}
            重新合成
          </Button>
        )}
      </div>
    </div>
  );
}

/** 成片弹窗：分集卡片点「播放成片」打开 */
export function FinalDialog({
  open,
  onOpenChange,
  ...props
}: FinalVideoProps & { open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>第 {props.no} 集成片</DialogTitle>
        </DialogHeader>
        <FinalVideo {...props} />
      </DialogContent>
    </Dialog>
  );
}
