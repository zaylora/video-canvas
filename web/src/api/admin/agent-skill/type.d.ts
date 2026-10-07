/**
 * Agent 技能管理接口类型。契约见 docs/design/Agent技能管理/Agent技能管理.md 第 8.2 节，
 * 字段与 backend/internal/service/agent/skill_types.go 的 json tag 一一对应。
 * 和 api/admin/ai、api/admin/storage 一样直接沿用后端 snake_case 字段，不做 camelCase 映射：
 * 字段与后端一一对应，映射层只会增加漂移风险。
 * 后端可能把空数组 / 空对象序列化成 null，API 层统一补成 []、{}，所以这里的数组字段都不会是 null。
 */

/** 技能来源 */
export type SkillSource =
  /** 随版本发布的内置技能，只读 */
  | "builtin"
  /** 管理员导入的技能 */
  | "imported";

/** 包内文件的类型 */
export type SkillFileKind =
  /** SKILL.md 本体 */
  | "skill"
  /** 脚本文件（第 1 期只读，不执行） */
  | "script"
  /** 文档 */
  | "doc"
  /** 资源文件 */
  | "asset"
  /** 其他 */
  | "other";

/** 预检问题级别 */
export type SkillIssueLevel =
  /** 错误：阻断确认 */
  | "error"
  /** 提示：不阻断 */
  | "warn"
  /** 信息 */
  | "info";

/** 预检的一条问题 */
export type SkillIssue = {
  /** 级别 */
  level: SkillIssueLevel;
  /** 问题代码，如 BAD_NAME、MISSING_REF */
  code: string;
  /** 对应的文件路径；整包级问题没有 */
  path?: string;
  /** 给人看的说明 */
  message: string;
};

/** 包内一个文件的清单项 */
export type SkillFile = {
  /** 相对技能根目录的路径，使用 / */
  path: string;
  /** 字节数 */
  size: number;
  /** 内容哈希 */
  sha256?: string;
  /** 文件类型 */
  kind: SkillFileKind;
  /** 脚本语言，仅 script 有 */
  lang?: string;
  /** 是否可作为文本预览；false 即二进制或过大 */
  text: boolean;
};

/** 管理页列表里的一行（含只读内置技能） */
export type SkillItem = {
  /** 技能名，唯一标识 */
  name: string;
  /** 显示名 */
  title: string;
  /** 一句话说明 */
  description: string;
  /** 来源 */
  source: SkillSource;
  /** 是否只读（内置为 true） */
  readonly: boolean;
  /** 是否已启用 */
  enabled: boolean;
  /** 生效版本号；内置技能为 null */
  active_version: number | null;
  /** 最新版本号 */
  latest_version: number;
  /** 有比生效版本更新的版本等待启用 */
  pending_version: boolean;
  /** 版本数量 */
  version_count: number;
  /** 文件数 */
  file_count: number;
  /** 总字节数 */
  total_bytes: number;
  /** 是否含脚本 */
  has_scripts: boolean;
  /** 本系统未支持的 frontmatter 字段 */
  unsupported_fields: string[];
  /** 最近更新时间（ISO 字符串） */
  updated_at: string;
};

/** 版本列表里的一项 */
export type SkillVersionHead = {
  /** 版本号 */
  version: number;
  /** 是否生效版本 */
  active: boolean;
  /** 整包 sha256 */
  sha256: string;
  /** 该版本的说明 */
  description: string;
  /** 文件数 */
  file_count: number;
  /** 总字节数 */
  total_bytes: number;
  /** 是否含脚本 */
  has_scripts: boolean;
  /** 本版本未支持的字段 */
  unsupported_fields: string[];
  /** 导入人用户 ID */
  created_by: number;
  /** 导入时间（ISO 字符串） */
  created_at: string;
};

/** 技能详情：列表项加版本列表 */
export type SkillDetail = SkillItem & {
  /** 版本列表，最新在前；内置技能没有 */
  versions: SkillVersionHead[];
  /** 只有内置技能给正文（它没有版本可查看） */
  body?: string;
};

/** 一个版本的完整信息 */
export type SkillVersionView = SkillVersionHead & {
  /** 技能名 */
  name: string;
  /** frontmatter 全部字段 */
  frontmatter: Record<string, unknown>;
  /** SKILL.md 正文 */
  body: string;
  /** 文件清单 */
  files: SkillFile[];
  /** 预检问题 */
  issues: SkillIssue[];
};

/** 读出的一个包内文件：文本给内容，二进制只给大小 */
export type SkillFileContent = {
  /** 路径 */
  path: string;
  /** 字节数 */
  size: number;
  /** 是否二进制 */
  binary: boolean;
  /** 文本内容 */
  text?: string;
  /** 内容过长被截断 */
  truncated?: boolean;
};

/** 确认导入后会发生什么 */
export type SkillImportPlan = {
  /** create 新技能；new_version 已有技能的新版本；blocked 不能确认 */
  action: "create" | "new_version" | "blocked";
  /** 将产生的版本号；0 或缺省表示没有 */
  version?: number;
  /** new_version 时当前生效的版本（不会变） */
  active_version?: number | null;
  /** new_version 时技能当前是否启用 */
  enabled: boolean;
};

/** 上传并预检的结果；预检不通过也是 200，问题在 issues 里 */
export type SkillImportView = {
  /** 暂存 id；空串表示包完全不可读，没有暂存 */
  id: string;
  /** 暂存过期时间（ISO 字符串） */
  expires_at?: string;
  /** 技能名 */
  name: string;
  /** 显示名 */
  title: string;
  /** 说明 */
  description: string;
  /** frontmatter 全部字段 */
  frontmatter: Record<string, unknown>;
  /** 本系统未支持的字段 */
  unsupported_fields: string[];
  /** 文件清单 */
  files: SkillFile[];
  /** 是否含脚本 */
  has_scripts: boolean;
  /** 总字节数 */
  total_bytes: number;
  /** 预检问题 */
  issues: SkillIssue[];
  /** 是否允许确认导入 */
  can_confirm: boolean;
  /** 确认后会发生什么 */
  plan: SkillImportPlan;
};

/** 删除预检 */
export type SkillDeleteCheck = {
  /** 是否可以删除 */
  can_delete: boolean;
  /** 不能删除的原因 */
  reason?: string;
  /** 将一并删除的版本数 */
  version_count: number;
};

/** 列表的状态筛选（后端只认 enabled / disabled，内置在前端过滤） */
export type SkillStatusFilter = "enabled" | "disabled";

/** 导入请求：zip 单文件，或文件夹 / 单个 SKILL.md 展开的成对 files 与 paths */
export type SkillImportInput =
  | {
      /** zip 压缩包 */
      kind: "zip";
      /** 压缩包文件 */
      file: File;
    }
  | {
      /** 文件夹或单个 SKILL.md */
      kind: "files";
      /** 逐文件与其相对路径（含外层目录） */
      entries: { file: File; path: string }[];
    };
