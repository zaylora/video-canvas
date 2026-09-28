import type { HandleType, Node, Position, XYPosition } from "@xyflow/react";

import type {
  AnimatedSvgEdge,
  MediaType,
  NodeStatus,
} from "@/components/canvas";
import type { NodeKind } from "@/constants/canvas/node-library";

export type {
  NodeKind,
  NodeKindMeta,
} from "@/constants/canvas/node-library";
export type { ModelOption } from "@/constants/canvas/model-library";
export type { MediaType, NodeStatus };

/** 画布节点自身携带的数据 */
export type CanvasNodeData = {
  /** 节点种类 */
  kind: NodeKind;
  /** 节点标题 */
  label: string;
  /** 输入框里的提示词 */
  prompt?: string;
  /** 选中的模型 id，缺省按该种类清单的第一条处理 */
  model?: string;
  /** 产出进度，缺省按 idle 处理 */
  status?: NodeStatus;
  /** 出图结果或上传素材的地址，生成中为 null */
  src?: string | null;
  /** src 那份素材是图还是视频 */
  mediaType?: MediaType;
  /** 服务端素材记录 id；有它的 URL 才能安全持久化 */
  assetId?: string;
  /** 素材是从本机传进来的，不是模型生成的 */
  uploaded?: boolean;
  /** 上传的文件名，摆在素材下面 */
  fileName?: string;
  /** 文本节点生成出来的正文 */
  text?: string | null;
  /** 生成失败的原因，摆给用户看的那句 */
  error?: string | null;
};

/** 画布上的节点 */
export type CanvasNode = Node<CanvasNodeData, "canvas">;

/** 画布上的连线 */
export type CanvasEdge = AnimatedSvgEdge;

/** 拉线落到空白处时记下的线头来源，选完种类就照它接边 */
export type PendingConnection = {
  /** 拉出连线的节点 */
  nodeId: string;
  /** 拉出连线的 handle，null 表示节点的默认 handle */
  handleId: string | null;
  /** 拉出端是 source 还是 target：source 表示新节点接在下游 */
  handleType: HandleType;
  /** 拉出端节点的种类，决定菜单里哪些种类可点 */
  kind: NodeKind;
  /** 拉出端 handle 的屏幕坐标，用来补画引导线 */
  fromScreen: XYPosition;
  /** 拉出端 handle 的朝向，决定引导线的出线方向 */
  fromPosition: Position;
};

/** 双击画布或拉线落空时记录 */
export type CanvasMenuState = {
  /** 相对视口的鼠标坐标 */
  screen: XYPosition;
  /** 由 screen 换算出的画布坐标 */
  flow: XYPosition;
  /** 拉线落空时的线头来源，双击空白时为 null */
  connection: PendingConnection | null;
};

/** 上传完要摆给用户看的一句话：不合规是错，接不上线只是知会一声 */
export type UploadNotice = {
  tone: "error" | "info";
  text: string;
};

/** 收下一个上传文件的结果：认下来给素材，认不下给一句能直接摆出去的话 */
export type UploadTaken =
  | { mediaType: MediaType; src: string }
  | { error: string };
