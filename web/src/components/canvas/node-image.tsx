import { useState } from "react";
import { ImageOff } from "lucide-react";

import { cn } from "@/lib/utils";

type NodeImageProps = {
  src: string;
  alt: string;
  className?: string;
  compact?: boolean;
};

export function NodeImage(props: NodeImageProps) {
  return <NodeImageContent key={props.src} {...props} />;
}

function NodeImageContent({ src, alt, className, compact }: NodeImageProps) {
  const [failed, setFailed] = useState(false);
  if (failed) {
    return (
      <span
        role="img"
        aria-label="素材不可用"
        title="素材不可用"
        className={cn(
          "text-muted-foreground flex items-center gap-1 p-1.5 text-xs",
          compact && "justify-center",
          className,
        )}
      >
        <ImageOff className="size-4 shrink-0" />
        {!compact && "素材不可用"}
      </span>
    );
  }
  return (
    <img
      src={src}
      alt={alt}
      draggable={false}
      className={className}
      onError={() => setFailed(true)}
    />
  );
}
