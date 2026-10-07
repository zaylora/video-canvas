import type {
  SkillFile,
  SkillImportView,
  SkillIssue,
  SkillIssueLevel,
  SkillItem,
} from "@/api/admin/agent-skill/type.d";

/** 字节数换算成 B / KB / MB，1 位小数 */
export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1048576).toFixed(1)} MB`;
}

/**
 * 文件清单里的类型标签：说明 / 脚本·语言 / 二进制 / 文档 / 资源 / 其他。
 * 不能按文本预览的非说明文件一律标“二进制”，让管理员一眼知道 Agent 读不到它的内容
 */
export function fileTagLabel(file: SkillFile): string {
  if (file.kind === "skill") return "说明";
  if (file.kind === "script") return file.lang ? `脚本·${file.lang}` : "脚本";
  if (!file.text) return "二进制";
  if (file.kind === "doc") return "文档";
  if (file.kind === "asset") return "资源";
  return "其他";
}

/** 问题级别的中文名：文字和图标一起表达，不只靠颜色 */
export const ISSUE_LEVEL_LABEL: Record<SkillIssueLevel, string> = {
  error: "错误",
  warn: "提示",
  info: "信息",
};

const LEVEL_RANK: Record<SkillIssueLevel, number> = { error: 0, warn: 1, info: 2 };

/** 问题按严重程度排序：错误、提示、信息；同级保持原顺序。返回新数组 */
export function sortIssues(issues: readonly SkillIssue[] | null | undefined): SkillIssue[] {
  return [...(issues ?? [])].sort((a, b) => LEVEL_RANK[a.level] - LEVEL_RANK[b.level]);
}

/** 各级问题数量；problems 不含信息级（“问题 N”徽标用它） */
export function countIssues(issues: readonly SkillIssue[] | null | undefined) {
  const count = { error: 0, warn: 0, info: 0, problems: 0 };
  for (const item of issues ?? []) count[item.level] += 1;
  count.problems = count.error + count.warn;
  return count;
}

/** 每个文件路径上最严重的问题级别，给文件树画圆点；信息级和整包级问题不标记 */
export function issueLevelByPath(
  issues: readonly SkillIssue[] | null | undefined,
): Map<string, "error" | "warn"> {
  const map = new Map<string, "error" | "warn">();
  for (const item of issues ?? []) {
    if (!item.path || item.level === "info") continue;
    if (map.get(item.path) === "error") continue;
    map.set(item.path, item.level);
  }
  return map;
}

/** 文件树节点 */
export type FileTreeNode = {
  /** 目录或文件 */
  kind: "dir" | "file";
  /** 显示名（路径最后一段） */
  name: string;
  /** 完整路径；目录没有尾部斜杠 */
  path: string;
  /** 子节点，文件为空数组 */
  children: FileTreeNode[];
  /** 文件的清单项，目录没有 */
  file?: SkillFile;
};

const byName = (a: FileTreeNode, b: FileTreeNode) => a.name.localeCompare(b.name);

function sortNodes(nodes: FileTreeNode[], root: boolean) {
  const dirs = nodes.filter((n) => n.kind === "dir").sort(byName);
  const files = nodes.filter((n) => n.kind === "file").sort(byName);
  if (root) {
    const index = files.findIndex((n) => n.name.toLowerCase() === "skill.md");
    if (index > 0) files.unshift(...files.splice(index, 1));
  }
  for (const dir of dirs) dir.children = sortNodes(dir.children, false);
  return [...dirs, ...files];
}

/** 把扁平的文件清单整理成树：目录在前、文件在后；根下 SKILL.md 排在文件最前 */
export function buildFileTree(files: readonly SkillFile[] | null | undefined): FileTreeNode[] {
  const root: FileTreeNode[] = [];
  const dirs = new Map<string, FileTreeNode>();
  for (const file of files ?? []) {
    const parts = file.path.split("/");
    let level = root;
    let acc = "";
    for (let i = 0; i < parts.length - 1; i += 1) {
      acc = acc ? `${acc}/${parts[i]}` : parts[i];
      let dir = dirs.get(acc);
      if (!dir) {
        dir = { kind: "dir", name: parts[i], path: acc, children: [] };
        dirs.set(acc, dir);
        level.push(dir);
      }
      level = dir.children;
    }
    level.push({
      kind: "file",
      name: parts[parts.length - 1],
      path: file.path,
      children: [],
      file,
    });
  }
  return sortNodes(root, true);
}

/** 展开后看得见的行（带缩进层级），文件树渲染和 ↑↓ 切换文件都用它 */
export function visibleTreeRows(
  tree: readonly FileTreeNode[],
  collapsed: ReadonlySet<string>,
): { node: FileTreeNode; depth: number }[] {
  const rows: { node: FileTreeNode; depth: number }[] = [];
  const walk = (nodes: readonly FileTreeNode[], depth: number) => {
    for (const node of nodes) {
      rows.push({ node, depth });
      if (node.kind === "dir" && !collapsed.has(node.path)) walk(node.children, depth + 1);
    }
  };
  walk(tree, 0);
  return rows;
}

/** 在文本里找第一次出现 needle 的行号（从 1 起）；MISSING_REF 靠它定位到引用处 */
export function locateLine(text: string, needle: string): number | null {
  if (!needle) return null;
  const index = text.split("\n").findIndex((line) => line.includes(needle));
  return index < 0 ? null : index + 1;
}

/** 预检横幅的色调：有错误或不能确认为红，仅提示为黄，其余为绿 */
export function bannerTone(view: SkillImportView): "ok" | "warn" | "bad" {
  const count = countIssues(view.issues);
  if (count.error > 0 || !view.can_confirm) return "bad";
  return count.warn > 0 ? "warn" : "ok";
}

/** 确认按钮被禁用的原因；可以确认时为 null */
export function confirmBlockReason(view: SkillImportView): string | null {
  if (view.can_confirm) return null;
  const errors = countIssues(view.issues).error;
  return errors > 0 ? `有 ${errors} 个错误需要先处理` : "预检未通过，不能导入";
}

/** “将要发生什么”的一句话：被拦住时直接用后端给出的第一条错误说明 */
export function planSummary(view: SkillImportView): string {
  const { plan, name } = view;
  if (plan.action === "create") return `将创建技能 ${name}，v${plan.version ?? 1}，默认停用`;
  if (plan.action === "new_version") {
    const active = plan.active_version == null ? "" : `；当前生效版本仍是 v${plan.active_version}`;
    return `将为 ${name} 新增 v${plan.version ?? ""}${active}，需要你在详情里手动切换`;
  }
  return sortIssues(view.issues).find((i) => i.level === "error")?.message ?? "存在错误，无法导入";
}

/** 列表的状态筛选项 */
export type SkillListFilter = "all" | "enabled" | "disabled" | "builtin";

/** 按名称、技能名、说明搜索并按状态过滤 */
export function filterSkillItems(
  items: readonly SkillItem[],
  query: string,
  status: SkillListFilter,
): SkillItem[] {
  const q = query.trim().toLowerCase();
  return items.filter((item) => {
    if (status === "enabled" && !item.enabled) return false;
    if (status === "disabled" && item.enabled) return false;
    if (status === "builtin" && item.source !== "builtin") return false;
    if (!q) return true;
    return `${item.title} ${item.name} ${item.description}`.toLowerCase().includes(q);
  });
}

/** 生效版本列的内容：内置显示“—”；有更新版本未生效时附带“vN 待生效” */
export function versionBadge(item: SkillItem): { active: string; pending: string | null } {
  if (item.source === "builtin" || item.active_version == null) {
    return { active: "—", pending: null };
  }
  return {
    active: `v${item.active_version}`,
    pending: item.pending_version ? `v${item.latest_version} 待生效` : null,
  };
}

/** 启停开关被禁用的原因；可用时为 null。停用不受“没有生效版本”限制 */
export function toggleBlockReason(item: SkillItem): string | null {
  if (item.readonly) return "内置技能随版本发布，不能停用";
  if (!item.enabled && item.active_version == null) return "没有生效版本，无法启用";
  return null;
}

/** 删除技能被禁用的原因；可删除时为 null */
export function deleteBlockReason(item: SkillItem): string | null {
  if (item.readonly) return "内置技能不能删除";
  if (item.enabled) return "请先停用再删除";
  return null;
}

/**
 * 去掉文件开头的 frontmatter 块（--- … ---），渲染态只显示正文，头部字段另用卡片展示。
 * @returns 正文，以及被去掉的行数（源码行号对照用）
 */
export function stripFrontmatter(text: string): { body: string; skippedLines: number } {
  const normalized = text.replace(/^\uFEFF/, "");
  if (!/^---[ \t]*\r?\n/.test(normalized)) return { body: text, skippedLines: 0 };
  const lines = normalized.split(/\r?\n/);
  const end = lines.findIndex((line, i) => i > 0 && /^---[ \t]*$/.test(line));
  if (end < 0) return { body: text, skippedLines: 0 };
  return {
    body: lines
      .slice(end + 1)
      .join("\n")
      .replace(/^\n+/, ""),
    skippedLines: end + 1,
  };
}

/** 导入成功后的 toast 文案：新技能默认停用；同名技能新增版本但生效版本不变 */
export function importedMessage(view: SkillImportView): string {
  if (view.plan.action === "new_version") {
    return `已为「${view.name}」新增 v${view.plan.version ?? ""}，生效版本不变，需要在详情里手动切换`;
  }
  return `已导入「${view.name}」v${view.plan.version ?? 1}，默认停用`;
}

/** 后端给的 ISO 时间显示成“2026-10-07 12:30”；解析不了原样返回 */
export function formatDateTime(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** 版本在时间线上的状态：生效中、比生效版本新的待生效、更早的历史版本 */
export function versionState(
  version: number,
  activeVersion: number | null,
): "active" | "pending" | "history" {
  if (version === activeVersion) return "active";
  return activeVersion != null && version > activeVersion ? "pending" : "history";
}
