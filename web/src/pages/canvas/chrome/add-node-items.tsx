import { Upload } from "lucide-react";

import type { AddNodeMenuItem } from "@/components/canvas";
import { ADD_NODE_MENU, NODE_META, UPLOAD_ACTION } from "@/constants/canvas";
import type { NodeKind } from "@/types";

/**
 * 「添加节点」菜单的条目（设计稿原型）：四种生成节点在前，上传素材隔一条线放最后。
 * disabled 判断某种节点能不能点（拉线落空时只放行接得上的）。
 */
export function buildAddNodeItems(disabled: (kind: NodeKind) => boolean = () => false) {
  const items: AddNodeMenuItem[] = ADD_NODE_MENU.map((entry) => {
    const Icon = NODE_META.get(entry.kind)?.icon;
    return {
      value: entry.kind,
      label: entry.title,
      icon: Icon ? <Icon /> : undefined,
      disabled: disabled(entry.kind),
    };
  });
  items.push({
    value: UPLOAD_ACTION,
    label: "上传素材",
    icon: <Upload />,
    // 传进来的素材落成图片、视频或音频节点，三种都接不上就没法上传
    disabled: disabled("image") && disabled("video") && disabled("audio"),
    separated: true,
  });
  return items;
}
