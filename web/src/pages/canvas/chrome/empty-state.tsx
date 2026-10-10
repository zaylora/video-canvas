import { motion } from "motion/react";
import { Upload } from "lucide-react";

import { ADD_NODE_MENU, NODE_META } from "@/constants/canvas";
import { DURATION, EASE_OUT } from "@/lib/motion";
import type { NodeKind } from "@/types";

const QUICK_KINDS: NodeKind[] = ["image", "video", "script"];

/** 空画布正中的快捷卡片：点一张就在视口中心建节点并选中 */
export function EmptyState({
  onAdd,
  onUpload,
}: {
  onAdd: (kind: NodeKind) => void;
  onUpload: () => void;
}) {
  const cards = [
    ...ADD_NODE_MENU.filter((entry) => QUICK_KINDS.includes(entry.kind)).flatMap((entry) => {
      const meta = NODE_META.get(entry.kind);
      return meta
        ? [
            {
              key: entry.kind,
              icon: <meta.icon className="size-5" />,
              title: entry.title,
              hint: entry.description,
              onClick: () => onAdd(entry.kind),
            },
          ]
        : [];
    }),
    {
      key: "upload",
      icon: <Upload className="size-5" />,
      title: "上传素材",
      hint: "图片、视频、音频",
      onClick: onUpload,
    },
  ];

  return (
    <div className="pointer-events-none absolute inset-0 z-[5] grid place-content-center justify-items-center gap-5 px-4">
      <motion.div
        initial={{ opacity: 0, y: 8 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: DURATION.slow, ease: EASE_OUT }}
        className="grid justify-items-center gap-1.5 text-center"
      >
        <h2 className="text-lg font-semibold text-balance">从一个节点开始</h2>
        <p className="text-muted-foreground text-sm">点下面的卡片，或者双击画布空白处</p>
      </motion.div>
      <div className="canvas-overlay-interactive pointer-events-auto flex flex-wrap justify-center gap-3">
        {cards.map((card, index) => (
          <motion.button
            key={card.key}
            type="button"
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: DURATION.slow, ease: EASE_OUT, delay: 0.04 * (index + 1) }}
            whileHover={{ y: -2 }}
            whileTap={{ scale: 0.97 }}
            onClick={card.onClick}
            className="bg-card ring-border hover:ring-foreground/25 focus-visible:ring-node-ring/60 grid w-36 gap-2.5 rounded-2xl p-4 text-left shadow-sm ring-1 transition-shadow outline-none focus-visible:ring-2"
          >
            <span className="text-muted-foreground">{card.icon}</span>
            <span className="text-sm font-semibold">{card.title}</span>
            <span className="text-muted-foreground text-xs">{card.hint}</span>
          </motion.button>
        ))}
      </div>
    </div>
  );
}
