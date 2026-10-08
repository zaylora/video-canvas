import { Type } from "typebox";

/** 工具的数据定义。执行都回调 Go（桥），这里只管声明给模型看的名字、说明和参数形状。 */
const pos = Type.Object({ x: Type.Number(), y: Type.Number() });

const opSchema = Type.Object(
  {
    op: Type.Union(
      ["create_node", "update_node", "create_group", "set_group", "connect", "move"].map((v) => Type.Literal(v)),
      { description: "操作种类" },
    ),
    tempId: Type.Optional(Type.String({ description: "新建时的临时 id，同一次调用里的后续操作可以用它引用新建的对象" })),
    id: Type.Optional(Type.String({ description: "目标节点或组的 id（update_node / set_group / move），也可以是前面操作的 tempId" })),
    kind: Type.Optional(Type.Union(["script", "image", "video", "audio"].map((v) => Type.Literal(v)), { description: "create_node：节点种类，script 是文本" })),
    label: Type.Optional(Type.String({ description: "标题，最多 40 字" })),
    prompt: Type.Optional(Type.String({ description: "提示词" })),
    model: Type.Optional(Type.String({ description: "生成模型 key，来自 model_list" })),
    params: Type.Optional(Type.Record(Type.String(), Type.Any(), { description: "生成参数，按模型的参数名" })),
    parentGroup: Type.Optional(Type.String({ description: "create_node：放进这个组（id 或 tempId）" })),
    position: Type.Optional(pos),
    color: Type.Optional(Type.String({ description: "组背景色：red/orange/yellow/green/cyan/blue/purple/pink，空串清除" })),
    labelColor: Type.Optional(Type.String({ description: "组名颜色，取值同 color" })),
    memberIds: Type.Optional(Type.Array(Type.String(), { description: "create_group：初始成员" })),
    addMembers: Type.Optional(Type.Array(Type.String(), { description: "set_group：加入的成员" })),
    removeMembers: Type.Optional(Type.Array(Type.String(), { description: "set_group：移出的成员" })),
    source: Type.Optional(Type.String({ description: "connect：起点节点" })),
    target: Type.Optional(Type.String({ description: "connect：终点节点" })),
  },
  { additionalProperties: false },
);

/** 全部工具的声明；run.mjs 按 Go 给的 allowed_tools 过滤 */
export const TOOL_DEFS = [
  {
    name: "canvas_get_state",
    label: "读取画布",
    description: "读画布。不传参数返回目录（每个节点的 id、种类、标题、所属组、是否已有产物、提示词摘要，选中的节点排在最前）；传 nodeIds 或 groupId 返回节点的完整数据和连线。需要节点的完整提示词和参数时用它，不要猜。",
    parameters: Type.Object({
      nodeIds: Type.Optional(Type.Array(Type.String(), { description: "要读的节点，最多 50 个" })),
      groupId: Type.Optional(Type.String({ description: "读这个组的全部成员" })),
    }),
  },
  {
    name: "canvas_apply_ops",
    label: "修改画布",
    description: "对画布做一批编辑，一次最多 30 项，整批要么全部生效要么都不生效（失败时会列出每一项的问题）。可以新建节点和组、改标题提示词模型参数、连线、移动、增减组成员。产物字段（图片、视频地址）不能写；已有产物的重做请新建节点。连线规则：文本→全部；图片→图片/视频；视频→视频；音频→视频。",
    parameters: Type.Object({ ops: Type.Array(opSchema, { description: "操作列表" }) }),
  },
  {
    name: "canvas_arrange",
    label: "整理布局",
    description: "整理布局：把一个组的成员或一组节点排成一行、一列或网格。",
    parameters: Type.Object({
      groupId: Type.Optional(Type.String({ description: "排列这个组的成员，排完后组框自动贴合" })),
      nodeIds: Type.Optional(Type.Array(Type.String(), { description: "排列这些节点" })),
      layout: Type.Union(["row", "column", "grid"].map((v) => Type.Literal(v)), { description: "row 横排 / column 竖排 / grid 网格" }),
    }),
  },
  {
    name: "canvas_delete",
    label: "删除",
    description: "申请删除节点或连线。不会直接删除：会向用户展示确认卡片，本轮暂停，用户决定后你会收到结果。只在用户明确要求清理或你确信是废稿时使用。",
    parameters: Type.Object({
      nodeIds: Type.Optional(Type.Array(Type.String())),
      edgeIds: Type.Optional(Type.Array(Type.String())),
      reason: Type.String({ description: "给用户看的理由" }),
    }),
  },
  {
    name: "plan_update",
    label: "更新计划",
    description: "展示和更新你的执行计划（最多 12 步）。开始较长的任务前先列计划，每完成一步更新状态。",
    parameters: Type.Object({
      steps: Type.Array(
        Type.Object({
          title: Type.String({ description: "这一步做什么，80 字以内" }),
          status: Type.Union(["todo", "doing", "done"].map((v) => Type.Literal(v))),
        }),
      ),
    }),
  },
  {
    name: "ask_user",
    label: "向用户提问",
    description: "信息不够时向用户提问，而不是猜。kind=choice 给 2 到 4 个选项；kind=model 让用户从已发布的图片或视频模型里选一个。提问后本轮暂停，用户回答后你会收到结果。",
    parameters: Type.Object({
      question: Type.String(),
      kind: Type.Union([Type.Literal("choice"), Type.Literal("model")]),
      options: Type.Optional(Type.Array(Type.String(), { description: "kind=choice 的选项" })),
      modelKind: Type.Optional(Type.Union([Type.Literal("image"), Type.Literal("video")], { description: "kind=model 时选哪类模型" })),
      allowCustom: Type.Optional(Type.Boolean({ description: "是否允许用户自己输入答案" })),
    }),
  },
  {
    name: "model_list",
    label: "查看可用模型",
    description: "列出已发布的生成模型（图片、视频或音频）及其能力和价格摘要：支持的生成方式、参考素材、参数和可选值。建节点前用它挑模型、写对参数。",
    parameters: Type.Object({
      kind: Type.Union(["image", "video", "audio"].map((v) => Type.Literal(v))),
    }),
  },
  {
    name: "generate_media",
    label: "生成",
    description: "为画布上的节点申请生成（图片、视频、音频、文本）。不会直接生成：会向用户展示确认卡片（含预估积分），本轮暂停，用户批准后任务才创建，你会收到结果。节点必须已选好模型、写好提示词，且还没有产物（不会覆盖已有产物，需要新版本请先新建节点）。上游连线的文字和素材会自动作为提示词补充和参考。一个节点只生成一个结果，要多个结果请建多个节点。",
    parameters: Type.Object({
      items: Type.Array(Type.Object({ nodeId: Type.String() }), { description: "要生成的节点，最多 8 个" }),
      reason: Type.Optional(Type.String({ description: "给用户看的说明" })),
    }),
  },
  {
    name: "task_get",
    label: "查询任务",
    description: "查询一个生成任务的状态、进度、失败原因和产物。taskId 是节点数据里记录的任务 id，或批准生成后结果里的 task_id。",
    parameters: Type.Object({ taskId: Type.String() }),
  },
  {
    name: "skill_search",
    label: "搜索技能",
    description: "在技能库（内置影视技能加管理员导入的技能）里按关键词找方法说明（剧本拆镜、角色三视图、场景设定、关键帧提示词、视频运镜提示词等）。不传关键词列出前几个。",
    parameters: Type.Object({ query: Type.Optional(Type.String({ description: "关键词，空格分隔" })) }),
  },
  {
    name: "skill_read",
    label: "读取技能",
    description: "读一个技能的完整方法说明；技能带资源文件时，正文末尾会列出清单，再传 file 读其中的文本文件。做拆分镜、写角色、场景、关键帧、视频提示词这类事之前先读对应技能。用户消息里的 @[名字](skill:key) 就是在指定要用的技能，key 即技能名。",
    parameters: Type.Object({
      name: Type.String({ description: "技能名，如 script-breakdown" }),
      file: Type.Optional(Type.String({ description: "技能包内的文件路径（相对技能根目录，取自正文末尾的资源清单），不传读正文" })),
    }),
  },
  {
    name: "canvas_inspect_image",
    label: "查看图片",
    description: "查看图片节点当前的图片内容（最多 4 张）。标题和提示词不等于画面：判断画面是否符合要求、人物是否一致、要不要重做之前，先看图。只能看已经有图片的图片节点。",
    parameters: Type.Object({ nodeIds: Type.Array(Type.String(), { description: "图片节点 id，1 到 4 个" }) }),
  },
];

/**
 * 生成 pi 的工具对象：execute 只是回调 Go。工具层的失败（参数不对、校验不过）是 is_error 的普通结果，
 * 让模型看到并自己改正；回调本身失败（网络、令牌无效）才抛异常，pi 会把它变成给模型的错误结果。
 */
export function buildTools(bridge, allowed, onTerminate = () => {}) {
  const allow = allowed ? new Set(allowed) : null;
  return TOOL_DEFS.filter((d) => !allow || allow.has(d.name)).map((d) => ({
    ...d,
    execute: async (toolCallId, params, signal) => {
      const r = await bridge.tool(toolCallId, d.name, params, signal);
      if (r.terminate) onTerminate();
      // 对象存储的图片只有地址：先放进 data，发请求前由 restoreImageUrls 还原
      const images = (r.images ?? []).map((i) => ({ type: "image", data: i.url ?? i.data, mimeType: i.mime_type }));
      return { content: [{ type: "text", text: r.content }, ...images], details: {}, isError: !!r.is_error, terminate: !!r.terminate };
    },
  }));
}
