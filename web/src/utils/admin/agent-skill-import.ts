import type { SkillImportView } from "@/api/admin/agent-skill/type.d";
import { formatSize, sortIssues } from "./agent-skill";

/** 导入限额：前端上传前先拦，后端预检也有同样的规则 */
export const IMPORT_LIMITS = {
  /** zip 压缩包最大字节数 */
  zipBytes: 50 * 1048576,
  /** 文件夹（解压后）总字节数上限 */
  folderBytes: 100 * 1048576,
  /** 单个文件最大字节数 */
  fileBytes: 25 * 1048576,
  /** 文件个数上限 */
  files: 500,
} as const;

/** 路径统一成 / 分隔、去掉开头的 ./ 和 /、合并重复斜杠 */
export function normalizeEntryPath(raw: string): string {
  return raw
    .replace(/\\/g, "/")
    .replace(/\/{2,}/g, "/")
    .replace(/^(\.\/)+/, "")
    .replace(/^\/+/, "");
}

/** 系统垃圾文件：后端预检同样会忽略，前端先丢掉，免得 .git 之类把文件数撑爆 */
function isJunkPath(path: string): boolean {
  const parts = path.split("/");
  const last = parts[parts.length - 1];
  return (
    parts.includes("__MACOSX") ||
    parts.includes(".git") ||
    last === ".DS_Store" ||
    last === "Thumbs.db"
  );
}

/** 选好的一份包：一个 zip，或一组带相对路径的文件（文件夹 / 单个 SKILL.md） */
export type PreparedPick<F> =
  | {
      /** zip 压缩包 */
      kind: "zip";
      /** 压缩包文件 */
      file: F;
    }
  | {
      /** 文件夹或单个 SKILL.md */
      kind: "files";
      /** 逐文件与其整理后的相对路径 */
      entries: { file: F; path: string }[];
      /** 被前端忽略的系统垃圾文件数 */
      ignored: number;
    };

/** preparePick 的结果：通过，或带原因和下一步的失败 */
export type PrepareResult<F> =
  | { ok: true; input: PreparedPick<F> }
  | { ok: false; error: string; hint?: string };

/**
 * 把用户选的 / 拖入的文件整理成一次上传，并按限额先拦：
 * 单个 .zip ≤ 50 MB；文件夹总量 ≤ 100 MB、≤ 500 个文件、单文件 ≤ 25 MB。
 * @param items 选中的文件及其相对路径（单个文件的路径就是文件名，文件夹里的文件含外层目录）
 */
export function preparePick<F extends { name: string; size: number }>(
  items: readonly { file: F; path: string }[],
): PrepareResult<F> {
  if (items.length === 0) {
    return { ok: false, error: "没有读到任何文件", hint: "请重新选择，或把文件拖到这里" };
  }
  const isZip = (item: { file: F; path: string }) => /\.zip$/i.test(item.file.name);
  const first = items[0];
  if (items.length === 1 && isZip(first) && !normalizeEntryPath(first.path).includes("/")) {
    if (first.file.size > IMPORT_LIMITS.zipBytes) {
      return {
        ok: false,
        error: `压缩包超过 ${IMPORT_LIMITS.zipBytes / 1048576} MB（当前 ${formatSize(first.file.size)}）`,
        hint: "请去掉不需要的大文件后重新打包",
      };
    }
    return { ok: true, input: { kind: "zip", file: first.file } };
  }
  if (items.some(isZip)) {
    return {
      ok: false,
      error: "一次只能导入一个技能",
      hint: "请只选择一个压缩包、一个文件夹或一个 SKILL.md",
    };
  }

  const entries: { file: F; path: string }[] = [];
  let ignored = 0;
  for (const item of items) {
    const path = normalizeEntryPath(item.path);
    if (!path || isJunkPath(path)) {
      ignored += 1;
      continue;
    }
    entries.push({ file: item.file, path });
  }
  if (entries.length === 0) {
    return { ok: false, error: "没有可导入的文件", hint: "选中的内容全是系统文件，已自动忽略" };
  }
  if (entries.length > IMPORT_LIMITS.files) {
    return {
      ok: false,
      error: `文件超过 ${IMPORT_LIMITS.files} 个（当前 ${entries.length} 个）`,
      hint: "请去掉不需要的文件后重试",
    };
  }
  const tooBig = entries.find((e) => e.file.size > IMPORT_LIMITS.fileBytes);
  if (tooBig) {
    return {
      ok: false,
      error: `「${tooBig.path}」超过单文件 ${IMPORT_LIMITS.fileBytes / 1048576} MB`,
      hint: "请去掉这个大文件后重试",
    };
  }
  const total = entries.reduce((sum, e) => sum + e.file.size, 0);
  if (total > IMPORT_LIMITS.folderBytes) {
    return {
      ok: false,
      error: `文件夹总大小超过 ${IMPORT_LIMITS.folderBytes / 1048576} MB（当前 ${formatSize(total)}）`,
      hint: "请去掉不需要的大文件后重试",
    };
  }
  return { ok: true, input: { kind: "files", entries, ignored } };
}

/** 导入状态机的状态：第 1 步（选择 / 上传中）与第 2 步（检查并确认） */
export type ImportState =
  | {
      /** 第 1 步：等待选择 */
      step: "pick";
      /** 拖放区里的红色提示（失败原因 + 下一步）；没有为 null */
      notice: string | null;
    }
  | {
      /** 第 1 步：上传并预检中 */
      step: "uploading";
      /** 正在处理的文件名或目录名 */
      name: string;
      /** upload 上传中；check 传完了，等后端解压检查 */
      phase: "upload" | "check";
      /** 上传进度，0 到 1，只增不减 */
      progress: number;
    }
  | {
      /** 第 2 步：预检结果 */
      step: "review";
      /** 预检结果（有错误也停在这里，让管理员看问题） */
      view: SkillImportView;
      /** 确认中：按钮转圈，其余禁用 */
      confirming: boolean;
    };

/** 导入状态机的事件 */
export type ImportAction =
  /** 开始上传一份包 */
  | { type: "start"; name: string }
  /** 上传进度，ratio 为 0 到 1 */
  | { type: "progress"; ratio: number }
  /** 后端返回了预检结果 */
  | { type: "uploaded"; view: SkillImportView }
  /** 上传或预检请求失败 */
  | { type: "failed"; message: string }
  /** 用户取消上传 */
  | { type: "cancelled" }
  /** 前端上传前校验不通过 */
  | { type: "rejected"; message: string }
  /** 点“确认导入” */
  | { type: "confirm" }
  /** 确认失败（过期、同名内置、内容相同等） */
  | { type: "confirmFailed"; message: string }
  /** 点“重新选择” */
  | { type: "reselect" }
  /** 整个流程重置（关闭对话框后） */
  | { type: "reset" };

/** 初始状态 */
export const initialImportState: ImportState = { step: "pick", notice: null };

/** 对话框头部步骤条的当前步：选择文件与上传中都是 1 */
export const stepOf = (state: ImportState): 1 | 2 => (state.step === "review" ? 2 : 1);

/**
 * 导入状态机：选择 → 上传 → 检查 → 确认。纯函数，副作用（请求、取消、删除暂存）在 use-skill-import 里。
 * 不合法的转移（上传中又 start、确认中又重选）原样返回旧状态，避免重复请求。
 */
export function importReducer(state: ImportState, action: ImportAction): ImportState {
  switch (action.type) {
    case "start":
      if (state.step === "uploading" || (state.step === "review" && state.confirming)) return state;
      return { step: "uploading", name: action.name, phase: "upload", progress: 0 };
    case "progress": {
      if (state.step !== "uploading") return state;
      const progress = Math.max(state.progress, Math.min(1, action.ratio));
      return { ...state, progress, phase: progress >= 1 ? "check" : state.phase };
    }
    case "uploaded": {
      if (state.step !== "uploading") return state;
      if (!action.view.id) {
        const reason = sortIssues(action.view.issues).find((i) => i.level === "error")?.message;
        return {
          step: "pick",
          notice: reason ?? "无法读取这个包，请确认是有效的技能压缩包、文件夹或 SKILL.md",
        };
      }
      return { step: "review", view: action.view, confirming: false };
    }
    case "failed":
      return state.step === "uploading" ? { step: "pick", notice: action.message } : state;
    case "cancelled":
      return state.step === "uploading" ? { step: "pick", notice: null } : state;
    case "rejected":
      return state.step === "pick" ? { step: "pick", notice: action.message } : state;
    case "confirm":
      if (state.step !== "review" || state.confirming || !state.view.can_confirm) return state;
      return { ...state, confirming: true };
    case "confirmFailed":
      return state.step === "review" && state.confirming
        ? { step: "pick", notice: action.message }
        : state;
    case "reselect":
      return state.step === "review" && !state.confirming ? { step: "pick", notice: null } : state;
    case "reset":
      return initialImportState;
  }
}

/** 业务错误码：导入暂存已过期或不存在 */
const IMPORT_EXPIRED_CODE = 61001;

/**
 * 确认失败时留在拖放区的说明：以后端 message 为主；暂存过期补一句下一步，没有 message 用兜底文案。
 */
export function confirmFailureNotice(error: unknown): string {
  const e =
    typeof error === "object" && error !== null
      ? (error as { code?: unknown; message?: unknown })
      : {};
  const message = typeof e.message === "string" ? e.message : "";
  if (!message) return "导入失败，请重新选择文件";
  return e.code === IMPORT_EXPIRED_CODE ? `${message}，请重新选择文件` : message;
}

/** FileSystemEntry 里读目录要用到的最小形状，方便不依赖浏览器做单测 */
export type FsEntryLike = {
  /** 是否文件 */
  isFile: boolean;
  /** 是否目录 */
  isDirectory: boolean;
  /** 名字 */
  name: string;
  /** 从拖入根开始的完整路径，以 / 开头 */
  fullPath: string;
  /** 文件条目：取 File */
  file?: (ok: (file: File) => void, fail: (error: unknown) => void) => void;
  /** 目录条目：创建读取器 */
  createReader?: () => {
    readEntries: (ok: (entries: FsEntryLike[]) => void, fail: (error: unknown) => void) => void;
  };
};

/** 读一个目录的全部子项：浏览器一次最多给 100 条，要反复读到返回空数组为止 */
async function readAllChildren(dir: FsEntryLike): Promise<FsEntryLike[]> {
  const reader = dir.createReader?.();
  if (!reader) return [];
  const all: FsEntryLike[] = [];
  for (;;) {
    const batch = await new Promise<FsEntryLike[]>((ok, fail) => reader.readEntries(ok, fail));
    if (batch.length === 0) return all;
    all.push(...batch);
  }
}

/**
 * 递归读出拖入的文件和文件夹，返回逐文件与相对路径（含外层目录，不带开头斜杠）。
 * 某个文件读不到就整体失败，不悄悄丢文件。
 */
export async function readDroppedEntries(
  roots: readonly FsEntryLike[],
): Promise<{ file: File; path: string }[]> {
  const out: { file: File; path: string }[] = [];
  const visit = async (entry: FsEntryLike): Promise<void> => {
    if (entry.isFile && entry.file) {
      const file = await new Promise<File>((ok, fail) => entry.file?.(ok, fail));
      out.push({ file, path: normalizeEntryPath(entry.fullPath) });
    } else if (entry.isDirectory) {
      for (const child of await readAllChildren(entry)) await visit(child);
    }
  };
  for (const root of roots) await visit(root);
  return out;
}
