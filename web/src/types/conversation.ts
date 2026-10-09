/** 创作模式：Agent 自己拆分镜选模型，其余三种直接出对应类型的内容 */
export type CreationMode = "agent" | "image" | "video" | "audio";

/** 生成结果的画幅：决定结果块的宽高比 */
export type ConversationResultShape = "square" | "wide" | "tall";

/** 一条对话记录里的一个生成结果（目前只有占位封面，接口就绪后换成素材地址） */
export type ConversationResult = {
  /** 结果类型 */
  kind: "image" | "video" | "audio";
  /** 画幅，音频忽略 */
  shape: ConversationResultShape;
  /** 占位封面的色相，0–360；真图到位后不再使用 */
  hue: number;
  /** 缩略尺寸：Agent 分镜图这类一行放好几张的场景 */
  small?: boolean;
  /** 分镜序号，从 1 开始；没有表示不是分镜 */
  shot?: number;
  /** 时长文案，如「0:05」；图片没有 */
  duration?: string;
};

/** 对话里的一条生成记录：一次提交对应一条 */
export type ConversationRecord = {
  /** 记录 ID */
  id: string;
  /** 日期标题，如「10月6日」；同一天的相邻记录只在第一条上方显示 */
  day: string;
  /** 这条记录用的创作模式 */
  mode: CreationMode;
  /** 用户写的提示词，技能以「/技能名」开头 */
  prompt: string;
  /** 模型、比例、时长、清晰度等参数文案，按展示顺序，用竖线分隔 */
  meta: string[];
  /** 参考图的占位色相；空数组表示没有参考图 */
  refHues: number[];
  /** Agent 的文字回复，只有 agent 模式有 */
  reply?: string;
  /** 生成结果，按展示顺序 */
  results: ConversationResult[];
  /** 完成后的下一步按钮文案，如分镜出完后的「生成 4 段视频」；没有就不显示 */
  next?: string;
  /** 生成进度 0–100；null 表示已完成 */
  progress: number | null;
};

/** 一段对话：侧栏里的一项，对应一页按日期分组的生成记录 */
export type Conversation = {
  /** 对话 ID */
  id: string;
  /** 标题 */
  title: string;
  /** 侧栏缩略图的占位色相；null 表示用图标 */
  hue: number | null;
  /** 记录，按时间正序 */
  records: ConversationRecord[];
};
