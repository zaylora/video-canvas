import type { SegmentShot } from "@/api/drama/type";

/** 分镜表（只读，供参考）：真正提交的是上面的提示词 */
export function ShotsTable({ shots }: { shots: readonly SegmentShot[] }) {
  if (shots.length === 0) return null;
  const row = "grid grid-cols-[54px_48px_64px_1fr] items-start gap-2.5 px-3 text-[12.5px]";
  return (
    <section aria-label="分镜表" className="mt-5">
      <div className="border-border overflow-hidden rounded-xl border">
        <div className={`${row} bg-muted text-muted-foreground py-1.5 text-[11px]`}>
          <span>分镜</span>
          <span>时长</span>
          <span>景别</span>
          <span>动作与对白</span>
        </div>
        {shots.map((shot, index) => (
          <div key={index} className={`${row} border-border border-t py-2`}>
            <span>分镜 {index + 1}</span>
            <span className="tabular-nums">{shot.seconds}s</span>
            <span>{shot.shotSize}</span>
            <span className="min-w-0">
              {shot.action}
              {shot.camera && <span className="text-muted-foreground">（{shot.camera}）</span>}
              {shot.dialogue && (
                <span className="text-muted-foreground block">「{shot.dialogue}」</span>
              )}
            </span>
          </div>
        ))}
      </div>
      <p className="text-muted-foreground mt-1.5 text-[11.5px]">
        分镜表来自分镜脚本，供参考；真正提交的是上面的提示词。
      </p>
    </section>
  );
}
