/** 内置影视技能的清单：与后端 backend/internal/agentskills/skills 一一对应（name 就是技能名，Agent 靠它 skill_read） */
export const AGENT_SKILLS: ReadonlyArray<{
  /** 技能名（目录名） */
  name: string;
  /** 中文名，chip 上显示 */
  title: string;
  /** 一句话说明 */
  desc: string;
}> = [
  {
    name: "script-breakdown",
    title: "剧本拆镜",
    desc: "按场次和镜头拆分，给出景别、运镜、画面、台词、时长，并对应到镜头组",
  },
  {
    name: "character-turnaround",
    title: "角色三视图",
    desc: "给每个角色出一张正面、侧面、背面参考图，保持前后一致",
  },
  {
    name: "scene-setting",
    title: "场景设定图",
    desc: "给每个主要场景出一张空镜参考图，说明空间、光线和氛围",
  },
  {
    name: "keyframe-prompt",
    title: "关键帧提示词",
    desc: "把一个镜头写成能稳定出图的提示词，含景别、构图、光线、风格",
  },
  {
    name: "video-motion-prompt",
    title: "视频运镜提示词",
    desc: "描述镜头怎么动、人物做什么，附不同模型的写法差异",
  },
];

/** 按名称、技能名、说明过滤；空关键词返回全部 */
export function filterSkills(query: string) {
  const q = query.trim().toLowerCase();
  if (!q) return AGENT_SKILLS;
  return AGENT_SKILLS.filter((s) => `${s.title} ${s.name} ${s.desc}`.toLowerCase().includes(q));
}
