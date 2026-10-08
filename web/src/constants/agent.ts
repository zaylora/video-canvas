import type { AgentMode } from "@/api/agent/type";

/** 任务模式：名称、一句说明（模式下拉里显示） */
export const AGENT_MODES: ReadonlyArray<{ value: AgentMode; label: string; hint: string }> = [
  { value: "all", label: "全能创作", hint: "按你的想法搭建、修改、整理画布，需要时申请生成" },
  { value: "script", label: "剧本创编", hint: "只写剧本和台词，不拆镜头、不建图片视频节点" },
  { value: "storyboard", label: "分镜搭建", hint: "把剧本拆成角色组和镜头组，连好参考关系" },
  { value: "prompt", label: "提示词优化", hint: "只改已有节点的提示词，不新建不删除" },
];

/** 工具的中文名，工具行显示用；不认识的工具显示原名 */
export const TOOL_LABELS: Record<string, string> = {
  canvas_get_state: "读取画布",
  canvas_apply_ops: "修改画布",
  canvas_arrange: "整理布局",
  canvas_delete: "申请删除",
  canvas_inspect_image: "查看图片",
  plan_update: "更新计划",
  ask_user: "向你提问",
  model_list: "查看可用模型",
  generate_media: "申请生成",
  task_get: "查询任务",
  skill_search: "搜索技能",
  skill_read: "读取技能",
};

/** 工具的短动词：活动块收起后的摘要按它计数，如「读取 2 · 修改 3」 */
export const TOOL_VERBS: Record<string, string> = {
  canvas_get_state: "读取",
  canvas_apply_ops: "修改",
  canvas_arrange: "整理",
  canvas_delete: "删除",
  canvas_inspect_image: "看图",
  plan_update: "计划",
  ask_user: "提问",
  model_list: "查模型",
  generate_media: "生成",
  task_get: "查任务",
  skill_search: "技能",
  skill_read: "技能",
};

/** 运行状态给用户看的话（只列需要在消息流里提示的） */
export const RUN_STATUS_TEXT: Record<string, { title: string; hint: string }> = {
  failed: { title: "运行失败", hint: "可以换个说法重试" },
  timeout: { title: "运行超时", hint: "可以继续，或换个说法重试" },
  interrupted: { title: "运行被中断", hint: "服务重启或运行进程退出了，可以从断点继续" },
  budget_exhausted: { title: "本轮预算用完了", hint: "追加预算后可以继续" },
  step_limit: { title: "达到本轮步数上限", hint: "继续的话会再给一轮步数" },
  canceled: { title: "已停止", hint: "没做完的计划还留着，可以接着让它做" },
  expired: { title: "等待超时", hint: "你太久没有回应，这一轮已经结束" },
};

/** 引导项（空会话时显示）：点击后的动作由面板决定 */
export const AGENT_GUIDES = [
  {
    id: "inspect",
    icon: "scan",
    title: "感知画布开始创作",
    hint: "读一遍画布，说说现在有什么、还缺什么",
    mode: "all" as AgentMode,
  },
  {
    id: "storyboard",
    icon: "film",
    title: "从剧本开始拆分镜",
    hint: "角色组、镜头组和参考线一次搭好",
    mode: "storyboard" as AgentMode,
  },
  {
    id: "story",
    icon: "upload",
    title: "上传故事来改编",
    hint: "支持 .txt / .md，先改成剧本",
    mode: "script" as AgentMode,
  },
  {
    id: "polish",
    icon: "wand",
    title: "批量优化提示词",
    hint: "只改提示词，不动画布结构",
    mode: "prompt" as AgentMode,
  },
] as const;
