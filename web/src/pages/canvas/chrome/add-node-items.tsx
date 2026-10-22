import { Library, Upload, UserRound } from "lucide-react";

import type { AddNodeMenuItem } from "@/components/canvas";
import {
  ADD_NODE_MENU,
  ASSET_LIBRARY_ACTION,
  CHARACTER_ACTION,
  NODE_META,
  UPLOAD_ACTION,
} from "@/constants/canvas";
import type { NodeKind } from "@/types";

/**
 * 「添加节点」菜单的条目（设计稿原型）：四种生成节点在前，上传素材隔一条线放后面，
 * 紧接着是「从资产库添加」「角色卡」（和上传同属「引入」一组）。
 * 角色卡要先选卡再建节点，不能像生成节点那样直接落一个空节点，所以是动作项，不在 ADD_NODE_MENU 里。
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
  // 同样不按拉线上下文限制：选卡后只建节点，连线由用户自己拉
  items.push({ value: CHARACTER_ACTION, label: "角色卡", icon: <UserRound /> });
  return items;
}
