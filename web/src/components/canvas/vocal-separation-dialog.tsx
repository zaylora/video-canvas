import { useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useRemoteModels } from "@/hooks/use-models";
import { useTaskGeneration } from "@/hooks/use-task-generation";
import type { CanvasNodeData } from "@/types";
import { quote } from "@/utils/pricing/quote";
import { priceSpecOf } from "@/utils/tasks/capabilities";

export function VocalSeparationDialog({
  id,
  data,
  source,
  onClose,
}: {
  id: string;
  data: CanvasNodeData;
  source: string;
  onClose: () => void;
}) {
  const remote = useRemoteModels("audio", true, "vocal_separation");
  const generation = useTaskGeneration(id, "audio");
  const [modelKey, setModelKey] = useState("");
  const [error, setError] = useState("");
  const model = remote.models.find((entry) => entry.key === modelKey) ?? remote.models[0];
  const input = { audio_operation: "vocal_separation", audios: [source] };
  const credits = model
    ? quote(model.pricing, model.capabilities, priceSpecOf(model.capabilities, input))
    : undefined;
  const blocked =
    generation.generationBlocked ??
    (data.assetId !== source
      ? "来源音频已变更，请重新打开后确认"
      : data.status === "running"
        ? "此节点正在生成"
        : remote.status === "error"
          ? "模型清单加载失败"
          : remote.status !== "ready"
            ? "正在加载模型"
            : !model
              ? "当前站点暂无可用的人声分离模型"
              : null);

  const submit = async () => {
    if (blocked || !model || generation.submitting) return;
    setError("");
    const result = await generation.submit({
      modelKey: model.key,
      input,
      currentSrc: data.src,
      expectedCredits: credits,
    });
    if (result.ok) onClose();
    else setError(result.error.message);
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !generation.submitting) onClose();
      }}
    >
      <DialogContent showCloseButton={!generation.submitting}>
        <DialogHeader>
          <DialogTitle>人声分离</DialogTitle>
          <DialogDescription>生成独立的人声和伴奏音轨，原音频保留在节点历史中。</DialogDescription>
        </DialogHeader>
        {model && (
          <label className="flex items-center justify-between gap-3">
            模型
            <select
              className="bg-chrome-hover border-chrome-border min-w-0 rounded-full border px-3 py-2"
              value={model.key}
              disabled={generation.submitting}
              onChange={(event) => setModelKey(event.target.value)}
            >
              {remote.models.map((entry) => (
                <option key={entry.key} value={entry.key}>
                  {entry.label}
                </option>
              ))}
            </select>
          </label>
        )}
        {blocked && (
          <p role="status" className="text-muted-foreground">
            {blocked}
          </p>
        )}
        {error && (
          <p role="alert" className="text-destructive">
            {error}
          </p>
        )}
        <DialogFooter>
          <Button variant="outline" disabled={generation.submitting} onClick={onClose}>
            取消
          </Button>
          <Button disabled={!!blocked || generation.submitting} onClick={() => void submit()}>
            {generation.submitting
              ? "提交中"
              : credits === undefined
                ? "确认分离"
                : `确认分离 · 预计 ${credits} 积分`}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
