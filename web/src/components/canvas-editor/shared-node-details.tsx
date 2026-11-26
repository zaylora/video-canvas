import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { useLinkedNodeView } from "@/hooks/use-linked-node-view";
import type { CanvasNodeData, MediaType } from "@/types";
import type { NodeStoryboard, StoryboardShot } from "@/types/storyboard";
import { activeOutputIdOf, readOutputs } from "@/utils/canvas/outputs";
import { shotImageState } from "@/utils/storyboard/shot-image";
import { formatShotNumber } from "@/utils/storyboard/shots";

function SharedMedia({ src, kind }: { src?: string | null; kind?: MediaType }) {
  const [failed, setFailed] = useState(false);
  if (!src) return <span className="text-muted-foreground">素材不可用</span>;
  if (failed)
    return (
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-muted-foreground">素材不可用</span>
        <Button type="button" variant="ghost" size="sm" onClick={() => setFailed(false)}>
          重试
        </Button>
      </div>
    );
  const onError = () => setFailed(true);
  if (kind === "audio")
    return <audio controls preload="none" src={src} onError={onError} className="w-full" />;
  if (kind === "video")
    return (
      <video controls preload="metadata" src={src} onError={onError} className="max-h-80 w-full" />
    );
  return (
    <img
      loading="lazy"
      alt=""
      src={src}
      onError={onError}
      className="max-h-80 w-full object-contain"
    />
  );
}

export function SharedNodeDetails({
  data,
  onClose,
}: {
  data: CanvasNodeData;
  onClose: () => void;
}) {
  const [selected, setSelected] = useState<string>();
  const outputs = readOutputs(data);
  const output =
    outputs.find((item) => item.id === selected) ??
    outputs.find((item) => item.id === activeOutputIdOf(data));
  const prompt = typeof data.params?.prompt === "string" ? data.params.prompt : data.prompt;
  const character = data.character;
  const references = Object.entries(data.paramAssets ?? {});
  const src = output?.src ?? data.src;
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        className="max-h-[85dvh] overflow-auto sm:max-w-2xl"
        onDoubleClick={(event) => event.stopPropagation()}
      >
        <DialogHeader>
          <DialogTitle>{data.label}</DialogTitle>
        </DialogHeader>
        {outputs.length > 1 && (
          <label className="flex items-center gap-2">
            历史结果
            <select
              className="bg-muted rounded-md p-1"
              value={output?.id}
              onChange={(event) => setSelected(event.target.value)}
            >
              {outputs.map((item, index) => (
                <option key={item.id} value={item.id}>
                  第 {index + 1} 版
                </option>
              ))}
            </select>
          </label>
        )}
        {src && <SharedMedia key={src} src={src} kind={output?.mediaType ?? data.mediaType} />}
        {prompt && (
          <div className="space-y-1">
            <strong>提示词</strong>
            <p className="whitespace-pre-wrap break-words">{prompt}</p>
          </div>
        )}
        {character && (
          <div className="space-y-2">
            <strong>
              {character.name} · {character.look}
            </strong>
            <p className="whitespace-pre-wrap">{character.description}</p>
            {character.voicePrompt && (
              <p className="whitespace-pre-wrap">{character.voicePrompt}</p>
            )}
            {character.voice && (
              <SharedMedia key={character.voice.src} src={character.voice.src} kind="audio" />
            )}
            {character.extras.map((item) => (
              <SharedMedia key={item.assetId} src={item.src} />
            ))}
          </div>
        )}
        {references.length > 0 && (
          <div className="space-y-2">
            <strong>参考素材</strong>
            {references.map(([key, item]) => (
              <SharedMedia key={`${key}:${item.url}`} src={item.url} kind={item.mediaType} />
            ))}
          </div>
        )}
        {!src && !prompt && !character && references.length === 0 && (
          <p className="text-muted-foreground">暂无已保存的内容</p>
        )}
      </DialogContent>
    </Dialog>
  );
}

export function SharedDetailsButton({
  data,
  label = "查看详情",
}: {
  data: CanvasNodeData;
  label?: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="nodrag nopan pointer-events-auto"
        onClick={() => setOpen(true)}
      >
        {label}
      </Button>
      {open && <SharedNodeDetails data={data} onClose={() => setOpen(false)} />}
    </>
  );
}

export function SharedStoryboardDialog({
  title,
  board,
  onClose,
}: {
  title: string;
  board: NodeStoryboard;
  onClose: () => void;
}) {
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        className="max-h-[85dvh] overflow-auto sm:max-w-6xl"
        onDoubleClick={(event) => event.stopPropagation()}
      >
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <div className="overflow-x-auto">
          <table className="w-full min-w-[48rem] text-left">
            <thead className="bg-muted">
              <tr>
                {[
                  "镜号",
                  "分镜图",
                  "景别",
                  "运镜",
                  "画面描述",
                  "动作 / 走位",
                  "对白",
                  "时长",
                  "视频",
                ].map((label) => (
                  <th key={label} scope="col" className="p-2">
                    {label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {board.shots.map((shot, index) => (
                <SharedShotRow key={shot.id} shot={shot} number={index + 1} />
              ))}
            </tbody>
          </table>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function SharedShotRow({ shot, number }: { shot: StoryboardShot; number: number }) {
  const linkedImage = useLinkedNodeView(shot.imageNodeId);
  const image = shotImageState(shot, linkedImage);
  const linkedVideo = useLinkedNodeView(shot.videoNodeId);
  const video = linkedVideo?.exists ? linkedVideo.view : undefined;
  const imageSrc =
    image.kind === "uploaded" || (image.kind === "node" && image.phase === "done")
      ? image.src
      : null;
  return (
    <tr className="border-b align-top">
      <th scope="row" className="p-2 tabular-nums">
        {formatShotNumber(number)}
      </th>
      <td className="w-28 p-2">
        {imageSrc && <SharedMedia key={imageSrc} src={imageSrc} kind="image" />}
      </td>
      {[shot.shot, shot.camera, shot.scene, shot.action, shot.dialogue, `${shot.duration} 秒`].map(
        (value, column) => (
          <td key={column} className="p-2 whitespace-pre-wrap">
            {value || "—"}
          </td>
        ),
      )}
      <td className="w-36 p-2">
        {video?.phase === "done" && <SharedMedia key={video.src} src={video.src} kind="video" />}
      </td>
    </tr>
  );
}
