import { Library, Upload } from "lucide-react";

import type { AddNodeMenuItem } from "@/components/canvas";
import { ADD_NODE_MENU, ASSET_LIBRARY_ACTION, NODE_META, UPLOAD_ACTION } from "@/constants/canvas";
import type { NodeKind } from "@/types";

/**
 * 「添加节点」菜单的条目（设计稿原型）：图片、视频、文本、音频、导演台、角色卡六种节点在前，
 * 上传素材隔一条线放后面，紧接着是「从资产库添加」（和上传同属「引入」一组）。
 * 角色卡和别的节点一样直接落一个空节点（角色数据只属于当前画布，没有「先选卡」这一步）。
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
  // 弹框不按拉线上下文限制类型，引入的素材只建节点不连线，所以不受 disabled 影响
  items.push({ value: ASSET_LIBRARY_ACTION, label: "从资产库添加", icon: <Library /> });
  return items;
}
