import { ApiError } from "@/utils/requests/request";
import service from "@/utils/requests/service";
import { getCurrentUserId } from "@/utils/storage/user-id";
import type { CanvasVersion } from "@/utils/canvas/collaboration-version";

export type SharingMode = "view" | "copy" | "transfer";
export type Viewer = {
  id: string;
  email: string;
  username?: string;
  role?: "editor" | "viewer";
  canGenerate?: boolean;
  canCopyToLibrary?: boolean;
};
export type SharedDrama = {
  id: string;
  title: string;
  cover_asset?: string;
  role?: "editor" | "viewer";
};
export type SharedEpisode = {
  no: number;
  title: string;
  script: string;
  final?: string;
  segments: { prompt: string; board?: string; videos: string[]; selected?: string }[];
};
export type SharedView = {
  title: string;
  outline: string;
  characters: string;
  asset_ids: string[];
  episodes: SharedEpisode[];
};

const pendingDeliveries = new Map<string, string>();

function deliveryUser() {
  try {
    return getCurrentUserId();
  } catch {
    return null;
  }
}

type PendingReceipt = {
  storageKey: string;
  key: string;
  id: string;
  email: string;
  mode: "copy" | "transfer";
};

export function pendingCanvasReceipts(): PendingReceipt[] {
  const owner = deliveryUser();
  if (!owner) return [];
  const keys = new Set(pendingDeliveries.keys());
  try {
    for (let index = 0; index < sessionStorage.length; index++) {
      const key = sessionStorage.key(index);
      if (key?.startsWith('["canvas-delivery",')) keys.add(key);
    }
  } catch {
    /* 仅使用内存里的请求 */
  }
  const items: PendingReceipt[] = [];
  for (const storageKey of keys) {
    try {
      const [kind, user, id, , mode, email] = JSON.parse(storageKey);
      const key = pendingDeliveries.get(storageKey) ?? readDeliveryKey(storageKey);
      if (
        kind === "canvas-delivery" &&
        user === owner &&
        typeof id === "string" &&
        typeof email === "string" &&
        (mode === "transfer" || mode === "copy") &&
        key
      )
        items.push({ storageKey, key, id, mode, email });
    } catch {
      /* 忽略旧版本或损坏的数据 */
    }
  }
  return items;
}

export async function checkCanvasReceipt(item: PendingReceipt) {
  const result = await service.get<{ status: "complete" | "running" | "failed" }>(
    `/canvas/${encodeURIComponent(item.id)}/deliveries/${encodeURIComponent(item.key)}`,
    undefined,
    { silent: true },
  );
  if (
    result.status !== "running" &&
    (pendingDeliveries.get(item.storageKey) ?? readDeliveryKey(item.storageKey)) === item.key
  )
    clearDelivery(item.storageKey);
  return result.status;
}

function clearDelivery(storageKey: string) {
  pendingDeliveries.delete(storageKey);
  storeDeliveryKey(storageKey, null);
}

function readDeliveryKey(key: string) {
  try {
    return sessionStorage.getItem(key);
  } catch {
    return null;
  }
}

function storeDeliveryKey(key: string, value: string | null) {
  try {
    if (value === null) sessionStorage.removeItem(key);
    else sessionStorage.setItem(key, value);
  } catch {
    // 禁用会话存储时，当前页面仍可通过内存记录重试。
  }
  if (key.startsWith('["canvas-delivery",') && typeof window !== "undefined")
    window.dispatchEvent(new window.Event("canvas-delivery-change"));
}

function sharing(resource: "dramas" | "canvas") {
  return {
    members: (id: string) => service.get<Viewer[]>(`/${resource}/${id}/shares`),
    remove: (id: string, member: string) =>
      service.delete<void>(`/${resource}/${id}/shares/${member}`),
    submit: async (id: string, mode: SharingMode, email: string, revision?: CanvasVersion) => {
      if (mode === "view") {
        await service.post<void>(`/${resource}/${id}/shares`, { mode, email }, { silent: true });
        return;
      }
      // 请求结果未确认时，重新打开弹窗也复用该次交付的请求键。
      const storageKey = JSON.stringify(
        resource === "dramas"
          ? ["drama-delivery", id, mode, email.toLowerCase()]
          : ["canvas-delivery", deliveryUser(), id, revision, mode, email.toLowerCase()],
      );
      const key =
        pendingDeliveries.get(storageKey) ??
        readDeliveryKey(storageKey) ??
        Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
          byte.toString(16).padStart(2, "0"),
        ).join("");
      pendingDeliveries.set(storageKey, key);
      storeDeliveryKey(storageKey, key);
      try {
        await service.post<void>(
          `/${resource}/${id}/shares`,
          { mode, email, request_key: key, ...(resource === "canvas" ? { revision } : {}) },
          {
            timeout: 5 * 60_000 + 5_000,
            silent: true,
          },
        );
        clearDelivery(storageKey);
      } catch (error) {
        if (error instanceof ApiError && (error.status === 0 || error.status >= 500)) {
          throw new Error(
            resource === "canvas"
              ? "交付结果暂时无法确认。可到「个人画布」核对结果。"
              : "交付结果暂时无法确认。请用相同邮箱和共享方式重试。",
          );
        }
        if (resource === "canvas" && error instanceof ApiError && error.code !== 30009)
          clearDelivery(storageKey);
        throw error;
      }
    },
    list: (keyword: string, page: number) =>
      service.get<{ list: SharedDrama[]; total: number; page: number; page_size: number }>(
        `/${resource}/shared`,
        {
          keyword,
          page,
          page_size: 24,
        },
      ),
  };
}
export const canvasSharing = sharing("canvas");
export const dramaSharing = {
  ...sharing("dramas"),
  read: (id: string) => service.get<SharedView>(`/dramas/${id}/reader`),
  media: (id: string, asset: string, signal: AbortSignal) =>
    service.get<Blob>(`/dramas/${id}/reader/media/${asset}`, undefined, {
      responseType: "blob",
      signal,
      timeout: 0,
    }),
};
