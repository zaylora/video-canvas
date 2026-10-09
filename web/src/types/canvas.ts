import type { HandleType, Node, Position, XYPosition } from "@xyflow/react";

import type { AnimatedSvgEdge, MediaType, NodeMediaType, NodeStatus } from "@/components/canvas";
import type { NodeKind } from "@/constants/canvas/node-library";

export type { NodeKind, NodeKindMeta } from "@/constants/canvas/node-library";
export type { ModelOption } from "@/constants/canvas/model-library";
export type { MediaType, NodeMediaType, NodeStatus };

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
  /** src 那份素材是图、视频还是音频 */
  mediaType?: NodeMediaType;
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
  /** 生成失败时的任务编号（task_ref），失败那一刻记下，重开画布后仍能拿它查日志 */
  errorTaskRef?: string | null;
  /** 视频生成任务 id（后端 id 是数字，这里统一存字符串）；status 为 running 时靠它对账回填 */
  taskId?: string;
  /** 按所选模型 capabilities 存的参数值：提示词 params.prompt（兼容旧的 prompt 字段）、生成方式 params.op、生成参数按参数名、手动添加的参考素材 params.images / videos / audios（素材 id 数组） */
  params?: Record<string, unknown>;
  /** params 里媒体字段所选素材的展示信息，字段名 -> 素材 */
  paramAssets?: Record<string, ParamAsset>;
  /** 历次生成的产物，旧的在前；src / mediaType / assetId 是当前那一版的镜像，下游照旧读它们 */
  outputs?: NodeOutput[];
  /** 当前选中的 outputs[].id */
  activeOutputId?: string;
};

/** 节点的一版生成产物 */
export type NodeOutput = {
  /** 版本 id，取素材 id，同一素材不会重复入列 */
  id: string;
  src: string;
  mediaType: NodeMediaType;
  assetId: string;
  /** 产出这一版的任务 */
  taskId?: string;
  /** 产出这一版的模型 */
  model?: string;
  /** 入列时间，毫秒 */
  createdAt: number;
};

/** 参数面板里手动选的素材，只用于展示，真正提交的是 params 里的 assetId */
export type ParamAsset = {
  url: string;
  fileName?: string;
  mediaType?: "image" | "video" | "audio";
};

/** 画布上的素材 / 生成节点 */
export type CanvasNode = Node<CanvasNodeData, "canvas">;

/** 组可选的颜色，对应 index.css 里的 --group-* token */
export type GroupHue = "red" | "orange" | "yellow" | "green" | "cyan" | "blue" | "purple" | "pink";

/** 组节点携带的数据：名字和两种颜色，成员关系靠成员的 parentId */
export type CanvasGroupData = {
  /** 组名 */
  label: string;
  /** 背景色，缺省为中性 */
  color?: GroupHue;
  /** 组名行的颜色，缺省为中性 */
  labelColor?: GroupHue;
};

/** 组：框住若干节点，整组一起拖；尺寸放在节点自身的 width / height 上，成员的 parentId 指向它 */
export type CanvasGroupNode = Node<CanvasGroupData, "group">;

/** 画布上所有种类的节点 */
export type FlowNode = CanvasNode | CanvasGroupNode;

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

/** 多选后从选区右侧拉出新节点时记下的来源：选完种类，每个接得上的节点各连一根线过去 */
export type PendingGroup = {
  /** 被引用的节点 */
  nodeIds: string[];
  /** 各节点出口的屏幕坐标，用来补画引导线 */
  froms: XYPosition[];
};

/** 双击画布、拉线落空或从多选区拉出时记录 */
export type CanvasMenuState = {
  /** 相对视口的鼠标坐标 */
  screen: XYPosition;
  /** 由 screen 换算出的画布坐标 */
  flow: XYPosition;
  /** 拉线落空时的线头来源，双击空白时为 null */
  connection: PendingConnection | null;
  /** 从多选区拉出时被引用的那组节点，其余情况没有 */
  group?: PendingGroup | null;
};

/** 上传完要摆给用户看的一句话：不合规是错，接不上线只是知会一声 */
export type UploadNotice = {
  /** 提示语气：error 表示不合规，info 表示知会一声 */
  tone: "error" | "info";
  /** 摆给用户看的文案 */
  text: string;
};

/** 收下一个上传文件的结果：认下来给素材，认不下给一句能直接摆出去的话 */
export type UploadTaken =
  /** 认下来：素材类型与本地预览地址 */
  | { mediaType: MediaType; src: string }
  /** 认不下：能直接展示给用户的原因 */
  | { error: string };
