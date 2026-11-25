import type { StorageHybridStatus } from "@/api/admin/storage/type.d";

/** 把限额表单中的十进制 GB 换算为接口字节数。无效输入返回 null。 */
export function parseHybridLimit(input: string): number | null {
  const text = input.trim();
  if (!/^(?:\d+(?:\.\d*)?|\.\d+)$/.test(text)) return null;
  const gb = Number(text);
  const bytes = Math.round(gb * 1_000_000_000);
  return Number.isSafeInteger(bytes) && (bytes > 0 || gb === 0) ? bytes : null;
}

/** 检测或启用期间，默认操作全部锁定，删除仅锁定入选云端。 */
export function hybridStorageLocks(status: StorageHybridStatus | null, id: number) {
  const locked = status?.state === "active" || status?.state === "preparing";
  return {
    defaultReason: locked ? "混合策略期间无法设置默认存储" : "",
    deleteReason: locked && status?.cloud_id === id ? "该云端存储正被混合策略使用" : "",
  };
}
