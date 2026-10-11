import { useMemo } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router";

/**
 * 模型编辑器“选中了什么”的来源与切换方式。
 * 模型页把它放在 URL 里（刷新、深链都能还原）；渠道页从弹窗里新建 / 编辑模型时放在页面自己的状态里，
 * 这样不用离开渠道页，关掉弹窗还能回到渠道的模型页签。
 */
export type ModelRoute = {
  /** 正在编辑的已有模型 key */
  key: string | null;
  /** 是否在新建 */
  isNew: boolean;
  /** 新建时带入的导入草稿 id */
  draftId: string | null;
  fromImport: boolean;
  /** 新建时预选的渠道 */
  presetChannel: string | null;
  /** 打开某个模型；test 为 true 时同时打开测试弹窗；replace 不留历史记录 */
  openModel: (key: string, options?: { test?: boolean; replace?: boolean }) => void;
  /** 进入新建 */
  openNew: () => void;
  /** 关闭编辑器 */
  close: (options?: { replace?: boolean }) => void;
};

/** 模型页用：选中状态在 URL 里（?key=<key>；/models/new 是新建） */
export function useUrlModelRoute(): ModelRoute {
  const navigate = useNavigate();
  const location = useLocation();
  const [params] = useSearchParams();
  const isNew = location.pathname.replace(/\/+$/, "").endsWith("/models/new");
  const key = isNew ? null : params.get("key");
  const draftId = isNew ? params.get("draft") : null;
  const fromImport = isNew && params.get("from") === "import";
  const presetChannel = isNew ? params.get("channel") : null;
  return useMemo(
    () => ({
      key,
      isNew,
      draftId,
      fromImport,
      presetChannel,
      openModel: (next, options) =>
        navigate(
          `/admin/ai/models?key=${encodeURIComponent(next)}${options?.test ? "&test=1" : ""}`,
          { replace: options?.replace },
        ),
      openNew: () => navigate("/admin/ai/models/new"),
      close: (options) => navigate("/admin/ai/models", { replace: options?.replace }),
    }),
    [key, isNew, draftId, fromImport, presetChannel, navigate],
  );
}
