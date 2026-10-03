import type { StoragePreset, StorageProvider, StorageView } from "@/api/admin-storage/type";
import { formatShortTime } from "@/utils/time";

import type { StorageFormField } from "./storage-form";

/** 已有素材引用后不能修改的定位字段：改了会让旧素材找不到文件 */
const LOCKED_FIELDS: readonly StorageFormField[] = [
  "region",
  "accountId",
  "endpoint",
  "bucket",
  "pathPrefix",
  "addressing",
];

/** 数字 → 千分位文本，如 1,284 */
export const formatCount = (n: number) => n.toLocaleString("zh-CN");

/**
 * 某个表单字段是否因为“已有素材引用”而锁定。
 * 服务商创建后永远不能改，那是另一个规则（编辑时直接禁用服务商卡片），不在这里。
 * @param field 表单字段
 * @param ctx editing 是否在编辑已有存储；locked 是存储视图里的 locked
 */
export function isFieldLocked(
  field: StorageFormField,
  ctx: { editing: boolean; locked: boolean },
): boolean {
  return ctx.editing && ctx.locked && LOCKED_FIELDS.includes(field);
}

/**
 * 锁定字段旁边的说明文案
 * @param assetCount 引用这套存储的素材数
 */
export const lockReason = (assetCount: number) =>
  `已有 ${formatCount(assetCount)} 个素材引用，修改会导致它们无法访问；如需换桶请新建存储`;

/**
 * 不能设为默认的原因；可以设为默认时返回 null。
 * 对象存储必须最近一次测试通过（没测过也不行），内置本地磁盘不受此限制。
 * @param view 存储视图
 */
export function defaultBlockReason(view: StorageView): string | null {
  if (view.is_default) return "已是默认存储";
  if (view.builtin) return null;
  if (!view.check) return "还没有测试通过，请先测试连接";
  if (!view.check.ok) return "最近一次测试未通过，不能设为默认";
  return null;
}

/**
 * 不能删除的原因；可以删除时返回 null。
 * 这是列表上的预判，进行中的上传等前端看不到的引用由删除预检接口补充。
 * @param view 存储视图
 */
export function deleteBlockReason(view: StorageView): string | null {
  if (view.builtin) return "内置存储不可删除";
  if (view.is_default) return "默认存储不可删除，请先把其他存储设为默认";
  if (view.asset_count > 0) return `被 ${formatCount(view.asset_count)} 个素材引用，不可删除`;
  return null;
}

/** 签名有效期 → 文本，如 15 分钟、1 小时 */
export function formatTtl(sec: number): string {
  if (sec % 3600 === 0) return `${sec / 3600} 小时`;
  if (sec % 60 === 0) return `${sec / 60} 分钟`;
  return `${sec} 秒`;
}

/** 签名有效期 → 列表里的短文本，如 15m、1h */
function shortTtl(sec: number): string {
  if (sec % 3600 === 0) return `${sec / 3600}h`;
  if (sec % 60 === 0) return `${sec / 60}m`;
  return `${sec}s`;
}

/** 下拉里固定提供的签名有效期（秒） */
const TTL_PRESETS = [900, 3600, 21600, 86400];

/**
 * 签名有效期下拉的选项：固定 4 档；已存的值不在档里时补上，避免打开表单就把它悄悄改掉。
 * @param current 当前值（秒）
 */
export function ttlOptions(current: number): { value: number; label: string }[] {
  const values = TTL_PRESETS.includes(current)
    ? TTL_PRESETS
    : [...TTL_PRESETS, current].sort((a, b) => a - b);
  return values.map((value) => ({ value, label: formatTtl(value) }));
}

/** 列表里“访问方式”列的文本 */
export function accessLabel(view: StorageView): string {
  if (view.provider === "local") return "/files 直出";
  if (view.access === "public") return "公开 · CDN";
  return `私有 · 签名 ${shortTtl(view.signed_ttl_sec)}`;
}

/**
 * “桶 · 地域”列：本地显示目录；对象存储第一行是桶名，第二行是地域与路径前缀（R2 写 R2 · auto）
 * @param view 存储视图
 */
export function storageLocation(view: StorageView): { primary: string; secondary: string } {
  if (view.provider === "local") return { primary: view.local_dir ?? "", secondary: "" };
  const where = view.provider === "r2" ? "R2 · auto" : view.region || view.endpoint;
  const prefix = view.path_prefix ? `/${view.path_prefix}` : "";
  return { primary: view.bucket, secondary: [where, prefix].filter(Boolean).join(" · ") };
}

/**
 * 服务商显示名：优先用预设接口给的名称（前端不写死），本地磁盘固定文案，找不到预设时回退到服务商 id
 * @param provider 服务商
 * @param presets 预设列表
 */
export function providerLabel(provider: StorageProvider, presets: StoragePreset[]): string {
  if (provider === "local") return "本地磁盘";
  return presets.find((item) => item.provider === provider)?.name ?? provider;
}

/** 连通状态的展示信息 */
export type CheckStatus = {
  /** 语气：success 正常，danger 失败，neutral 未测试或无需测试 */
  tone: "success" | "danger" | "neutral";
  /** 状态文案 */
  label: string;
  /** 补充说明：失败时是原因，正常时是测试时间 */
  detail: string;
};

/**
 * 最近一次连接测试 → 展示状态
 * @param view 存储视图
 */
export function checkStatus(view: StorageView): CheckStatus {
  const check = view.check;
  if (!check) {
    return {
      tone: "neutral",
      label: view.builtin ? "无需测试" : "未测试",
      detail: "",
    };
  }
  if (check.ok) return { tone: "success", label: "正常", detail: formatShortTime(check.at) };
  return { tone: "danger", label: "失败", detail: check.error };
}
