import type { CanvasArchiveTicketDto } from "@/api/canvas/type";

/** 分组文案的固定顺序，和后端压缩包的顶层分组一致 */
const GROUP_ORDER = ["剧本", "分镜表", "角色", "图片", "视频", "音频", "成片"];

/** 把字节数写成人看的单位，与后端清单口径一致 */
export function archiveSizeText(bytes: number): string {
  if (bytes >= 1 << 30) return `${(bytes / (1 << 30)).toFixed(2)} GB`;
  if (bytes >= 1 << 20) return `${(bytes / (1 << 20)).toFixed(1)} MB`;
  if (bytes >= 1 << 10) return `${Math.round(bytes / (1 << 10))} KB`;
  return `${bytes} B`;
}

/** 把「各组几个文件」拼成一句：只列有内容的组，按生产链路的顺序 */
export function archiveGroupsText(groups: Record<string, number>): string {
  return GROUP_ORDER.filter((name) => (groups[name] ?? 0) > 0)
    .map((name) => `${name} ${groups[name]}`)
    .join(" · ");
}

/** 打包下载流程需要的外部能力，由调用方注入，流程本身不依赖 React 和浏览器 */
export type CanvasArchiveDeps = {
  /** 要打包的画布 ID */
  canvasId: string;
  /** 向后端申请票据；注入便于测试 */
  request: (id: string) => Promise<CanvasArchiveTicketDto>;
  /** 把带票据的地址交给浏览器原生下载（不占 JS 内存） */
  open: (url: string) => void;
  /** 给用户的提示 */
  notify: {
    /** 已经开始下载 */
    success(message: string, description: string): void;
    /** 有文件被打包流程跳过 */
    warning(message: string): void;
  };
};

/** 一次打包的结果：已交给浏览器下载、或请求就失败了 */
export type ArchiveOutcome = "started" | "failed";

/**
 * 打包下载当前画布的全部素材：先申请票据，再把带票据的地址交给浏览器原生下载。
 *
 * 为什么不在前端自己 fetch 成 blob：画布素材在对象存储里，几十个文件几百 MB，
 * 全拉进内存再拼 zip 会卡死标签页；原生下载还能看到浏览器的进度和剩余时间。
 * @param deps 画布信息和注入的外部能力
 */
export async function runCanvasArchive(deps: CanvasArchiveDeps): Promise<ArchiveOutcome> {
  let ticket: CanvasArchiveTicketDto;
  try {
    ticket = await deps.request(deps.canvasId);
  } catch {
    // 请求层已经弹过错误提示，这里不再重复（coding-standards §3）
    return "failed";
  }
  deps.open(ticket.url);
  deps.notify.success(
    `开始打包 ${ticket.count} 个文件`,
    `${archiveGroupsText(ticket.groups)} · ${archiveSizeText(ticket.total_bytes)}，浏览器会保存为 ${ticket.file_name}`,
  );
  if (ticket.skipped > 0) {
    deps.notify.warning(`有 ${ticket.skipped} 项引用的文件已取不到，压缩包里会跳过`);
  }
  return "started";
}
