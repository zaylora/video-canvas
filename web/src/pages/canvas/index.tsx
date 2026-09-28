import { ReactFlowProvider } from "@xyflow/react";
import { useEffect, useState } from "react";
import { useParams } from "react-router";
import { getCanvas } from "@/api/canvas";
import type { CanvasDetailDto } from "@/api/canvas/type";

import { Flow } from "./flow";

export default function Canvas() {
  const { id } = useParams();
  const [canvas, setCanvas] = useState<CanvasDetailDto | null>(null);
  const [error, setError] = useState(false);
  useEffect(() => {
    let active = true;
    if (!id) return;
    void getCanvas(id).then((value) => { if (active) setCanvas(value); }).catch(() => {
      if (active) setError(true);
    });
    return () => { active = false; };
  }, [id]);
  if (!id) return null;
  if (error) return <div className="grid min-h-svh place-items-center">画布不存在或无法加载</div>;
  if (!canvas) return <div className="grid min-h-svh place-items-center text-muted-foreground">正在加载画布…</div>;
  return (
    <ReactFlowProvider>
      <Flow key={`${id}:${canvas.version}`} canvas={canvas} onConflict={setCanvas} />
    </ReactFlowProvider>
  );
}
